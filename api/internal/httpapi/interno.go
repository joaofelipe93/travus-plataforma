package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// tipoLiberado: tipos de execução que a API entrega ao worker. "real" só com
// LANCE_REAL_HABILITADO=true (o worker tem a sua própria chave).
func (s *Servidor) tipoLiberado(tipo string) bool {
	switch tipo {
	case "dry_run", "reimpressao":
		return true
	case "real":
		return s.cfg.LanceRealHabilitado
	}
	return false
}

// Situações que o worker pode informar pela rota /concluir, por tipo de execução. O resultado
// do lance real e da reimpressão tem rotas próprias (concluir-confirmacao e
// concluir-reimpressao); por aqui esses tipos só registram erro antes de confirmar.
var conclusoesPermitidas = map[string]map[string]bool{
	"dry_run":     {"verificada": true, "erro_antes_confirmar": true},
	"real":        {"erro_antes_confirmar": true},
	"reimpressao": {"erro_antes_confirmar": true},
}

var assinaturaPNG = []byte("\x89PNG\r\n\x1a\n")

const limiteScreenshot = 5 << 20

// RotasInternas: só para o worker. Servidas numa porta separada (API_ADDR_INTERNO), que o
// Traefik não roteia, e com token de serviço (WORKER_TOKEN).
func (s *Servidor) RotasInternas() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/tarefas/proxima", s.proximaTarefa)
	mux.HandleFunc("POST /internal/execucoes/{id}/renovar", s.renovarTrava)
	mux.HandleFunc("POST /internal/execucoes/{id}/eventos", s.registrarEventos)
	mux.HandleFunc("POST /internal/execucoes/{id}/finalizar", s.finalizarExecucao)
	mux.HandleFunc("POST /internal/execucoes/{id}/liberar", s.liberarExecucao)
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/iniciar", s.iniciarCota)
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/concluir", s.concluirCota)
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/screenshot", s.enviarScreenshot)
	// Etapa 3 (interno_real.go).
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/pdf", s.enviarPdf)
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/confirmacao-iniciada", s.marcarConfirmacaoIniciada)
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/concluir-confirmacao", s.concluirConfirmacao)
	mux.HandleFunc("POST /internal/execucao-cotas/{id}/concluir-reimpressao", s.concluirReimpressao)
	return recuperar(s.exigirTokenWorker(mux))
}

func (s *Servidor) exigirTokenWorker(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.cfg.TokenWorker == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.TokenWorker)) != 1 {
			responderErro(w, http.StatusUnauthorized, "token de serviço inválido")
			return
		}
		h.ServeHTTP(w, r)
	})
}

type respostaConflito struct {
	Erro      string `json:"erro"`
	Cancelada bool   `json:"cancelada,omitempty"`
	// Parar: a execução não está mais com este worker (trava perdida ou finalizada).
	Parar bool `json:"parar,omitempty"`
}

func nomeWorker(w http.ResponseWriter, r *http.Request) (string, bool) {
	nome := strings.TrimSpace(r.Header.Get("X-Worker"))
	if nome == "" || len(nome) > 100 {
		responderErro(w, http.StatusBadRequest, "cabeçalho X-Worker obrigatório")
		return "", false
	}
	return nome, true
}

func (s *Servidor) evento(ctx context.Context, q *db.Queries, execucaoID int64, cotaID *int64, nivel, mensagem string) error {
	_, err := q.InserirEvento(ctx, db.InserirEventoParams{
		ExecucaoID: execucaoID, ExecucaoCotaID: cotaID, Nivel: nivel, Mensagem: mensagem, Dados: []byte("{}"),
	})
	return err
}

// objetoJSON devolve o JSON recebido se ele for um objeto; senão, {}.
func objetoJSON(bruto json.RawMessage) []byte {
	var objeto map[string]any
	if len(bruto) > 0 && json.Unmarshal(bruto, &objeto) == nil && objeto != nil {
		return bruto
	}
	return []byte("{}")
}

type pedidoProxima struct {
	Tipos []string `json:"tipos"`
}

type tarefaJSON struct {
	Execucao struct {
		ID   int64  `json:"id"`
		Tipo string `json:"tipo"`
	} `json:"execucao"`
	Cotas []cotaExecucaoJSON `json:"cotas"`
}

// proximaTarefa devolve à fila execuções de workers que pararam de responder e entrega a
// próxima da fila (uma por credencial do Newcon). 204 quando não há nada.
func (s *Servidor) proximaTarefa(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p pedidoProxima
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"tipos": ["dry_run"]}`)
		return
	}
	var tipos []string
	for _, t := range p.Tipos {
		if s.tipoLiberado(t) {
			tipos = append(tipos, t)
		}
	}
	if len(tipos) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx := r.Context()

	if err := s.recuperarTravasVencidas(r); err != nil {
		erroInterno(w, r, err)
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	reservada, err := qtx.ReservarProximaExecucao(ctx, db.ReservarProximaExecucaoParams{Worker: &worker, Tipos: tipos})
	var pgErr *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pgErr) && pgErr.Code == "23505") {
		// Nada na fila, ou outra execução da mesma credencial acabou de começar.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	rows, err := qtx.ListarCotasDaExecucao(ctx, reservada.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := s.evento(ctx, qtx, reservada.ID, nil, "info", fmt.Sprintf("Worker %s começou a execução.", worker)); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}

	var t tarefaJSON
	t.Execucao.ID, t.Execucao.Tipo = reservada.ID, reservada.Tipo
	for _, c := range rows {
		t.Cotas = append(t.Cotas, cotaParaJSON(c))
	}
	responderJSON(w, http.StatusOK, t)
}

func (s *Servidor) recuperarTravasVencidas(r *http.Request) error {
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	if _, err := qtx.RecuperarCotasDeTravasVencidas(ctx); err != nil {
		return err
	}
	devolvidas, err := qtx.DevolverExecucoesComTravaVencida(ctx)
	if err != nil {
		return err
	}
	for _, d := range devolvidas {
		mensagem := "O worker parou de responder: a execução voltou para a fila e continua das cotas pendentes."
		if d.Status == "cancelada" {
			if _, err := qtx.CancelarCotasPendentes(ctx, d.ID); err != nil {
				return err
			}
			mensagem = "O worker parou de responder depois do pedido de cancelamento: execução cancelada."
		}
		if err := s.evento(ctx, qtx, d.ID, nil, "aviso", mensagem); err != nil {
			return err
		}
		slog.Warn("execução com trava vencida", "execucao", d.ID, "nova_situacao", d.Status)
	}
	return tx.Commit(ctx)
}

func (s *Servidor) renovarTrava(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	cancelar, err := s.q.RenovarTrava(r.Context(), db.RenovarTravaParams{ID: id, Worker: &worker})
	if errors.Is(err, pgx.ErrNoRows) {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a execução não está mais com este worker", Parar: true})
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]bool{"cancelamento_solicitado": cancelar})
}

// execucaoDoWorker trava a execução e confere se ela está em andamento com este worker.
func (s *Servidor) execucaoDoWorker(w http.ResponseWriter, r *http.Request, qtx *db.Queries, id int64, worker string) (db.TravarExecucaoRow, bool) {
	e, err := qtx.TravarExecucao(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return e, false
	}
	if err != nil {
		erroInterno(w, r, err)
		return e, false
	}
	if e.Status != "em_andamento" || e.Worker == nil || *e.Worker != worker {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a execução não está mais com este worker", Parar: true})
		return e, false
	}
	return e, true
}

type eventoWorker struct {
	ExecucaoCotaID *int64          `json:"execucao_cota_id"`
	Nivel          string          `json:"nivel"`
	Mensagem       string          `json:"mensagem"`
	Dados          json.RawMessage `json:"dados"`
}

func (s *Servidor) registrarEventos(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	var p struct {
		Eventos []eventoWorker `json:"eventos"`
	}
	if err := lerJSON(w, r, &p, 512<<10); err != nil || len(p.Eventos) > 500 {
		responderErro(w, http.StatusBadRequest, `envie {"eventos": [...]} (até 500)`)
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if _, ok := s.execucaoDoWorker(w, r, qtx, id, worker); !ok {
		return
	}
	for _, e := range p.Eventos {
		nivel := e.Nivel
		if nivel != "ok" && nivel != "aviso" && nivel != "erro" {
			nivel = "info"
		}
		mensagem := strings.ToValidUTF8(strings.TrimSpace(e.Mensagem), "")
		if mensagem == "" {
			continue
		}
		if utf8.RuneCountInString(mensagem) > 2000 {
			mensagem = string([]rune(mensagem)[:2000]) + "…"
		}
		if _, err := qtx.InserirEvento(ctx, db.InserirEventoParams{
			ExecucaoID: id, ExecucaoCotaID: e.ExecucaoCotaID, Nivel: nivel, Mensagem: mensagem, Dados: objetoJSON(e.Dados),
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" { // cota de outra execução
				responderErro(w, http.StatusBadRequest, "execucao_cota_id inválido")
				return
			}
			erroInterno(w, r, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cotaDoWorker trava a execução da cota e confere se está com este worker.
func (s *Servidor) cotaDoWorker(w http.ResponseWriter, r *http.Request, qtx *db.Queries, worker string) (db.BuscarExecucaoCotaParaWorkerRow, bool) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cota da execução não encontrada")
		return db.BuscarExecucaoCotaParaWorkerRow{}, false
	}
	c, err := qtx.BuscarExecucaoCotaParaWorker(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cota da execução não encontrada")
		return c, false
	}
	if err != nil {
		erroInterno(w, r, err)
		return c, false
	}
	if c.ExecucaoStatus != "em_andamento" || c.Worker == nil || *c.Worker != worker {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a execução não está mais com este worker", Parar: true})
		return c, false
	}
	return c, true
}

func tagDe(c db.BuscarExecucaoCotaParaWorkerRow) string {
	return c.Grupo + "-" + c.Cota + "-" + c.Versao
}

func (s *Servidor) iniciarCota(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	c, ok := s.cotaDoWorker(w, r, qtx, worker)
	if !ok {
		return
	}
	if c.CancelamentoSolicitado {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "execução cancelada: não comece outra cota", Cancelada: true})
		return
	}
	n, err := qtx.IniciarExecucaoCota(ctx, c.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		// Nunca reprocessa cota que já passou (ou está passando) pela confirmação.
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: fmt.Sprintf("a cota %s está em %q e não pode ser iniciada", tagDe(c), c.Status)})
		return
	}
	if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, "info", fmt.Sprintf("Cota %s: iniciando.", tagDe(c))); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type pedidoConcluir struct {
	Status   string          `json:"status"`
	ErroTipo string          `json:"erro_tipo"`
	Erro     string          `json:"erro"`
	Detalhes json.RawMessage `json:"detalhes"`
}

func (s *Servidor) concluirCota(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p pedidoConcluir
	if err := lerJSON(w, r, &p, 64<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "envie status, erro_tipo, erro e detalhes")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	c, ok := s.cotaDoWorker(w, r, qtx, worker)
	if !ok {
		return
	}
	if !conclusoesPermitidas[c.Tipo][p.Status] {
		responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("situação %q não é permitida numa execução %s", p.Status, c.Tipo))
		return
	}
	var erroTipo, erroTexto *string
	if p.Status == "erro_antes_confirmar" {
		if (p.ErroTipo != "conhecido" && p.ErroTipo != "inesperado") || strings.TrimSpace(p.Erro) == "" {
			responderErro(w, http.StatusUnprocessableEntity, `erro_antes_confirmar exige erro_tipo ("conhecido" ou "inesperado") e erro`)
			return
		}
		erroTipo, erroTexto = &p.ErroTipo, &p.Erro
	}

	n, err := qtx.ConcluirExecucaoCota(ctx, db.ConcluirExecucaoCotaParams{
		ID: c.ID, Status: p.Status, ErroTipo: erroTipo, Erro: erroTexto, Detalhes: objetoJSON(p.Detalhes),
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: fmt.Sprintf("a cota %s não está em andamento (está em %q)", tagDe(c), c.Status)})
		return
	}
	nivel, mensagem := "ok", fmt.Sprintf("Cota %s verificada: \"2º Fixo\" marcado, pronta para confirmar (dry-run, nada foi confirmado).", tagDe(c))
	if p.Status == "erro_antes_confirmar" {
		nivel, mensagem = "erro", fmt.Sprintf("Cota %s: %s", tagDe(c), p.Erro)
		if c.Tipo == "real" {
			mensagem += " (não cliquei em Confirmar)"
		}
	}
	if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, nivel, mensagem); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Servidor) enviarScreenshot(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limiteScreenshot)
	conteudo, err := io.ReadAll(r.Body)
	if err != nil {
		responderErro(w, http.StatusRequestEntityTooLarge, "screenshot maior que 5 MB")
		return
	}
	if !bytes.HasPrefix(conteudo, assinaturaPNG) {
		responderErro(w, http.StatusUnsupportedMediaType, "o screenshot precisa ser PNG")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	c, ok := s.cotaDoWorker(w, r, qtx, worker)
	if !ok {
		return
	}
	soma := sha256.Sum256(conteudo)
	expira := s.agora().Add(s.cfg.RetencaoScreenshots)
	arquivoID, err := qtx.InserirArquivo(ctx, db.InserirArquivoParams{
		Tipo: "screenshot", Nome: tagDe(c) + ".png", ContentType: "image/png",
		Tamanho: int32(len(conteudo)), Sha256: hex.EncodeToString(soma[:]), Conteudo: conteudo, ExpiraEm: &expira,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.DefinirScreenshotExecucaoCota(ctx, db.DefinirScreenshotExecucaoCotaParams{ScreenshotID: &arquivoID, ID: c.ID}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]string{"id": arquivoID})
}

func (s *Servidor) finalizarExecucao(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	var p struct {
		Erro string `json:"erro"`
	}
	if err := lerJSON(w, r, &p, 16<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {} ou {"erro": "..."}`)
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	e, ok := s.execucaoDoWorker(w, r, qtx, id, worker)
	if !ok {
		return
	}
	// Confirmação sem resultado informado: nunca volta a ser processada, vai para conferência.
	semResultado, err := qtx.MarcarConfirmacoesPendentesComoErro(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if semResultado > 0 {
		if err := s.evento(ctx, qtx, id, nil, "erro", fmt.Sprintf("%d cota(s) clicaram em Confirmar sem o resultado chegar à plataforma: confira no Histórico do Newcon.", semResultado)); err != nil {
			erroInterno(w, r, err)
			return
		}
	}
	resumo, err := qtx.ResumoCotasDaExecucao(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	erro := strings.TrimSpace(p.Erro)
	var status string
	switch {
	case erro != "":
		status = "falhou"
	case e.CancelamentoSolicitado:
		status = "cancelada"
		if _, err := qtx.CancelarCotasPendentes(ctx, id); err != nil {
			erroInterno(w, r, err)
			return
		}
	case resumo.Restantes > 0:
		status, erro = "falhou", fmt.Sprintf("o worker terminou sem processar %d cota(s)", resumo.Restantes)
	case resumo.ComErro > 0 || resumo.EmConfirmacao > 0:
		status = "concluida_com_erros"
	default:
		status = "concluida"
	}
	if err := qtx.FinalizarExecucao(ctx, db.FinalizarExecucaoParams{ID: id, Status: status, Erro: textoOuNil(erro)}); err != nil {
		erroInterno(w, r, err)
		return
	}
	nivel, mensagem := "ok", "Execução concluída."
	switch status {
	case "concluida_com_erros":
		nivel, mensagem = "aviso", fmt.Sprintf("Execução concluída com %d cota(s) com erro.", resumo.ComErro)
	case "cancelada":
		nivel, mensagem = "aviso", "Execução cancelada."
	case "falhou":
		nivel, mensagem = "erro", "Execução interrompida: "+erro
	}
	if err := s.evento(ctx, qtx, id, nil, nivel, mensagem); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]string{"status": status})
}

// liberarExecucao: o worker vai desligar (SIGTERM) depois de terminar a cota atual.
func (s *Servidor) liberarExecucao(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	if _, ok := s.execucaoDoWorker(w, r, qtx, id, worker); !ok {
		return
	}
	if _, err := qtx.DevolverCotasEmAndamento(ctx, id); err != nil {
		erroInterno(w, r, err)
		return
	}
	if _, err := qtx.DevolverExecucaoParaFila(ctx, db.DevolverExecucaoParaFilaParams{ID: id, Worker: &worker}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := s.evento(ctx, qtx, id, nil, "aviso", "O worker foi desligado: a execução voltou para a fila e continua das cotas pendentes."); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
