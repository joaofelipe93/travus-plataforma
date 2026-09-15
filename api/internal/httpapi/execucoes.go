package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const maxCotasPorExecucao = 500

// Situações finais de uma execução.
func execucaoFinalizada(status string) bool {
	switch status {
	case "concluida", "concluida_com_erros", "cancelada", "falhou":
		return true
	}
	return false
}

type execucaoResumoJSON struct {
	ID                     int64      `json:"id"`
	Tipo                   string     `json:"tipo"`
	Status                 string     `json:"status"`
	CriadaPorNome          string     `json:"criada_por_nome"`
	CriadaEm               time.Time  `json:"criada_em"`
	IniciadaEm             *time.Time `json:"iniciada_em"`
	FinalizadaEm           *time.Time `json:"finalizada_em"`
	CancelamentoSolicitado bool       `json:"cancelamento_solicitado"`
	Erro                   *string    `json:"erro"`
	Total                  int32      `json:"total"`
	Sucesso                int32      `json:"sucesso"`
	ComErro                int32      `json:"com_erro"`
	Restantes              int32      `json:"restantes"`
}

type cotaExecucaoJSON struct {
	ID           int64           `json:"id"`
	CotaID       int64           `json:"cota_id"`
	Ordem        int32           `json:"ordem"`
	Grupo        string          `json:"grupo"`
	Cota         string          `json:"cota"`
	Versao       string          `json:"versao"`
	ClienteNome  string          `json:"cliente_nome"`
	Modalidade   string          `json:"modalidade"`
	Status       string          `json:"status"`
	ErroTipo     *string         `json:"erro_tipo"`
	Erro         *string         `json:"erro"`
	Detalhes     json.RawMessage `json:"detalhes"`
	ScreenshotID *string         `json:"screenshot_id"`
	Tentativas   int32           `json:"tentativas"`
	IniciadaEm   *time.Time      `json:"iniciada_em"`
	FinalizadaEm *time.Time      `json:"finalizada_em"`
}

type pedidoExecucao struct {
	Tipo    string  `json:"tipo"`
	CotaIDs []int64 `json:"cota_ids"`
}

func (s *Servidor) criarExecucao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoExecucao
	if err := lerJSON(w, r, &p, 64<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"tipo": "dry_run", "cota_ids": [...]}`)
		return
	}
	if p.Tipo != "dry_run" {
		responderErro(w, http.StatusUnprocessableEntity, "só o dry-run está disponível: a execução real ainda não foi liberada")
		return
	}
	ids := make([]int64, 0, len(p.CotaIDs))
	vistos := map[int64]bool{}
	for _, id := range p.CotaIDs {
		if id > 0 && !vistos[id] {
			vistos[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		responderErro(w, http.StatusUnprocessableEntity, "escolha pelo menos uma cota")
		return
	}
	if len(ids) > maxCotasPorExecucao {
		responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("escolha no máximo %d cotas por execução", maxCotasPorExecucao))
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

	exec, err := qtx.CriarExecucao(ctx, db.CriarExecucaoParams{Tipo: p.Tipo, CriadaPor: u.ID})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	n, err := qtx.InserirCotasNaExecucao(ctx, db.InserirCotasNaExecucaoParams{ExecucaoID: exec.ID, CotaIds: ids})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if int(n) != len(ids) {
		responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("%d cota(s) escolhida(s) não existem ou estão inativas: atualize a lista e tente de novo", len(ids)-int(n)))
		return
	}
	if _, err := qtx.InserirEvento(ctx, db.InserirEventoParams{
		ExecucaoID: exec.ID, Nivel: "info", Dados: []byte("{}"),
		Mensagem: fmt.Sprintf("Dry-run criado por %s com %d cota(s). Nenhum lance será confirmado.", u.Nome, n),
	}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, &u.ID, "execucao_criada", "execucao", idTexto(exec.ID),
		map[string]any{"tipo": exec.Tipo, "cotas": n})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]any{"id": exec.ID, "tipo": exec.Tipo, "status": exec.Status})
}

func (s *Servidor) listarExecucoes(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	rows, err := s.q.ListarExecucoes(r.Context())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	out := make([]execucaoResumoJSON, 0, len(rows))
	for _, e := range rows {
		out = append(out, execucaoResumoJSON{
			ID: e.ID, Tipo: e.Tipo, Status: e.Status, CriadaPorNome: e.CriadaPorNome, CriadaEm: e.CriadaEm,
			IniciadaEm: e.IniciadaEm, FinalizadaEm: e.FinalizadaEm, CancelamentoSolicitado: e.CancelamentoSolicitado,
			Erro: e.Erro, Total: e.Total, Sucesso: e.Sucesso, ComErro: e.ComErro, Restantes: e.Restantes,
		})
	}
	responderJSON(w, http.StatusOK, map[string]any{"execucoes": out})
}

func (s *Servidor) buscarExecucao(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	ctx := r.Context()
	e, err := s.q.BuscarExecucao(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	rows, err := s.q.ListarCotasDaExecucao(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	cotas := make([]cotaExecucaoJSON, 0, len(rows))
	totais := map[string]int{}
	for _, c := range rows {
		totais[c.Status]++
		cotas = append(cotas, cotaExecucaoJSON{
			ID: c.ID, CotaID: c.CotaID, Ordem: c.Ordem, Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao,
			ClienteNome: c.ClienteNome, Modalidade: c.Modalidade, Status: c.Status, ErroTipo: c.ErroTipo,
			Erro: c.Erro, Detalhes: json.RawMessage(c.Detalhes), ScreenshotID: c.ScreenshotID, Tentativas: c.Tentativas,
			IniciadaEm: c.IniciadaEm, FinalizadaEm: c.FinalizadaEm,
		})
	}
	var posicao *int32
	if e.Status == "na_fila" {
		p, err := s.q.PosicaoNaFila(ctx, id)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		posicao = &p
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"execucao": map[string]any{
			"id": e.ID, "tipo": e.Tipo, "status": e.Status, "criada_por_nome": e.CriadaPorNome,
			"criada_em": e.CriadaEm, "iniciada_em": e.IniciadaEm, "finalizada_em": e.FinalizadaEm,
			"cancelamento_solicitado": e.CancelamentoSolicitado, "cancelada_por_nome": e.CanceladaPorNome,
			"erro": e.Erro, "posicao_fila": posicao,
		},
		"totais": totais,
		"cotas":  cotas,
	})
}

func (s *Servidor) cancelarExecucao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
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

	e, err := qtx.TravarExecucao(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}

	var resultado, mensagem string
	switch {
	case e.Status == "na_fila":
		if err := qtx.CancelarExecucaoNaFila(ctx, db.CancelarExecucaoNaFilaParams{UsuarioID: &u.ID, ID: id}); err != nil {
			erroInterno(w, r, err)
			return
		}
		if _, err := qtx.CancelarCotasPendentes(ctx, id); err != nil {
			erroInterno(w, r, err)
			return
		}
		resultado, mensagem = "cancelada", fmt.Sprintf("Cancelada por %s antes de começar.", u.Nome)
	case e.Status == "em_andamento" && !e.CancelamentoSolicitado:
		if err := qtx.SolicitarCancelamento(ctx, db.SolicitarCancelamentoParams{UsuarioID: &u.ID, ID: id}); err != nil {
			erroInterno(w, r, err)
			return
		}
		resultado, mensagem = "cancelando", fmt.Sprintf("Cancelamento pedido por %s: o worker termina a cota atual e para.", u.Nome)
	case e.Status == "em_andamento":
		responderJSON(w, http.StatusOK, map[string]any{"id": id, "status": "cancelando"})
		return
	default:
		responderErro(w, http.StatusConflict, "esta execução já terminou")
		return
	}

	if _, err := qtx.InserirEvento(ctx, db.InserirEventoParams{ExecucaoID: id, Nivel: "aviso", Mensagem: mensagem, Dados: []byte("{}")}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, &u.ID, "execucao_cancelada", "execucao", idTexto(id),
		map[string]any{"situacao_anterior": e.Status})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{"id": id, "status": resultado})
}

var formatoUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// baixarArquivo entrega screenshots só para quem tem sessão (mostram CPF e data de nascimento).
func (s *Servidor) baixarArquivo(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id := r.PathValue("id")
	if !formatoUUID.MatchString(id) {
		responderErro(w, http.StatusNotFound, "arquivo não encontrado")
		return
	}
	a, err := s.q.BuscarArquivo(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "arquivo não encontrado (screenshots são apagados depois de 30 dias)")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Conteudo)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", a.Nome))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.Conteudo)
}
