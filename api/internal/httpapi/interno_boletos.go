package httpapi

// Rotas internas do worker de boletos (porta 8081, token WORKER_TOKEN, sem rota no Traefik).
// O worker não fala com o banco: ele pede o próximo cliente, consulta o Boleto Fácil e devolve
// o que achou. O CPF sai daqui só para quem tem o token de serviço, e nunca é gravado fora de
// clientes.cpf_cnpj. Ver docs/canopus-boleto-facil.md e interno.go (o mesmo desenho de fila).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const (
	limitePdfBoleto = 10 << 20 // boleto é PDF pequeno; 10 MB é folga de sobra
	// Boleto velho não serve para nada e é dado de pagamento de cliente: sai do banco em 90 dias.
	// (O comprovante de lance, que é documento, não expira.)
	retencaoPdfBoleto = 90 * 24 * time.Hour
	maxParcelasPorVez = 60
)

// textoLimitado deixa a mensagem válida em UTF-8 e no tamanho que a coluna aguenta: o que vem
// do worker é texto de erro do site, que pode vir enorme ou truncado no meio de um caractere.
func textoLimitado(bruto string, limite int) string {
	texto := strings.TrimSpace(strings.ToValidUTF8(bruto, ""))
	if utf8.RuneCountInString(texto) > limite {
		texto = string([]rune(texto)[:limite])
	}
	return texto
}

// Situações que o worker pode informar ao concluir um cliente.
var conclusoesCliente = map[string]bool{"consultado": true, "sem_boleto": true, "erro": true}

func (s *Servidor) rotasInternasBoletos(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/boletos/rodadas/proxima", s.proximaRodada)
	mux.HandleFunc("POST /internal/boletos/rodadas/{id}/renovar", s.renovarRodada)
	mux.HandleFunc("POST /internal/boletos/rodadas/{id}/proximo-cliente", s.proximoClienteBoletos)
	mux.HandleFunc("POST /internal/boletos/rodadas/{id}/eventos", s.eventosBoletos)
	mux.HandleFunc("POST /internal/boletos/rodadas/{id}/finalizar", s.finalizarRodada)
	mux.HandleFunc("POST /internal/boletos/rodadas/{id}/liberar", s.liberarRodada)
	mux.HandleFunc("POST /internal/boletos/clientes/{id}/parcelas", s.registrarParcelas)
	mux.HandleFunc("POST /internal/boletos/clientes/{id}/concluir", s.concluirClienteBoletos)
	mux.HandleFunc("POST /internal/boletos/parcelas/{id}/pdf", s.enviarPdfBoleto)
}

func (s *Servidor) eventoBoleto(ctx context.Context, q *db.Queries, rodadaID int64, clienteID *int64, nivel, mensagem string) error {
	return q.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
		RodadaID: rodadaID, RodadaClienteID: clienteID, Nivel: nivel, Mensagem: mensagem, Dados: []byte("{}"),
	})
}

type rodadaTarefaJSON struct {
	Rodada struct {
		ID         int64  `json:"id"`
		Tipo       string `json:"tipo"`
		Referencia string `json:"referencia"`
	} `json:"rodada"`
}

// proximaRodada devolve à fila rodadas de workers que pararam e entrega a próxima. 204 quando
// não há nada. Uma rodada em andamento por vez: o site tem limite de taxa.
func (s *Servidor) proximaRodada(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := s.recuperarRodadasVencidas(r); err != nil {
		erroInterno(w, r, err)
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	reservada, err := qtx.ReservarProximaRodada(ctx, &worker)
	var pgErr *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pgErr) && pgErr.Code == "23505") {
		// Fila vazia, ou outra rodada acabou de começar (índice de rodada única).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := s.eventoBoleto(ctx, qtx, reservada.ID, nil, "info", fmt.Sprintf("Worker %s começou a rodada.", worker)); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}

	var t rodadaTarefaJSON
	t.Rodada.ID, t.Rodada.Tipo = reservada.ID, reservada.Tipo
	t.Rodada.Referencia = reservada.Referencia.Format("2006-01")
	responderJSON(w, http.StatusOK, t)
}

// recuperarRodadasVencidas: worker que parou de responder devolve a rodada à fila. Cliente que
// ficou em andamento volta a pendente — repetir a consulta de um cliente é seguro (nada no
// Boleto Fácil é irreversível; o pior caso é um generate-bill a mais).
func (s *Servidor) recuperarRodadasVencidas(r *http.Request) error {
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	if _, err := qtx.DevolverClientesComTravaVencida(ctx); err != nil {
		return err
	}
	devolvidas, err := qtx.DevolverRodadasComTravaVencida(ctx)
	if err != nil {
		return err
	}
	for _, d := range devolvidas {
		mensagem := "O worker parou de responder: a rodada voltou para a fila e continua dos clientes pendentes."
		if d.Status == "cancelada" {
			if _, err := qtx.CancelarClientesPendentes(ctx, d.ID); err != nil {
				return err
			}
			mensagem = "O worker parou de responder depois do pedido de cancelamento: rodada cancelada."
		}
		if err := s.eventoBoleto(ctx, qtx, d.ID, nil, "aviso", mensagem); err != nil {
			return err
		}
		slog.Warn("rodada de boletos com trava vencida", "rodada", d.ID, "nova_situacao", d.Status)
	}
	return tx.Commit(ctx)
}

func (s *Servidor) renovarRodada(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return
	}
	cancelar, err := s.q.RenovarTravaRodada(r.Context(), db.RenovarTravaRodadaParams{ID: id, Worker: &worker})
	if errors.Is(err, pgx.ErrNoRows) {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a rodada não está mais com este worker", Parar: true})
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]bool{"cancelamento_solicitado": cancelar})
}

// rodadaDoWorker confere se a rodada existe, está em andamento e é deste worker.
func (s *Servidor) rodadaDoWorker(w http.ResponseWriter, r *http.Request, q *db.Queries, id int64, worker string) (db.RodadaParaWorkerRow, bool) {
	rd, err := q.RodadaParaWorker(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return rd, false
	}
	if err != nil {
		erroInterno(w, r, err)
		return rd, false
	}
	if rd.Status != "em_andamento" || rd.Worker == nil || *rd.Worker != worker {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a rodada não está mais com este worker", Parar: true})
		return rd, false
	}
	return rd, true
}

// clienteDoWorker: o mesmo, pelo id do cliente na rodada.
func (s *Servidor) clienteDoWorker(w http.ResponseWriter, r *http.Request, q *db.Queries, id int64, worker string) (db.ClienteDaRodadaParaWorkerRow, bool) {
	c, err := q.ClienteDaRodadaParaWorker(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cliente da rodada não encontrado")
		return c, false
	}
	if err != nil {
		erroInterno(w, r, err)
		return c, false
	}
	if c.RodadaStatus != "em_andamento" || c.Worker == nil || *c.Worker != worker {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a rodada não está mais com este worker", Parar: true})
		return c, false
	}
	return c, true
}

type cotaBoletoJSON struct {
	ID     int64  `json:"id"`
	Grupo  string `json:"grupo"`
	Cota   string `json:"cota"`
	Versao string `json:"versao"`
}

type clienteBoletoJSON struct {
	Cliente struct {
		ID int64 `json:"id"`
		// Documento só com dígitos: é o que o Boleto Fácil aceita em /find-cota.
		Documento string           `json:"documento"`
		Nome      string           `json:"nome"`
		Cotas     []cotaBoletoJSON `json:"cotas"`
	} `json:"cliente"`
}

// proximoClienteBoletos entrega o próximo cliente pendente da rodada, já marcado como em
// andamento, com o documento e as cotas cadastradas. 204 quando não há mais nada a fazer
// (inclusive quando o cancelamento foi pedido).
func (s *Servidor) proximoClienteBoletos(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	if _, ok := s.rodadaDoWorker(w, r, qtx, id, worker); !ok {
		return
	}
	prox, err := qtx.ProximoClienteDaRodada(ctx, db.ProximoClienteDaRodadaParams{RodadaID: id, Worker: &worker})
	if errors.Is(err, pgx.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	linhas, err := qtx.IniciarRodadaCliente(ctx, prox.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		// Outro ciclo pegou o mesmo cliente: o worker pede de novo.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	cotas, err := qtx.CotasDoCliente(ctx, prox.ClienteID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}

	var out clienteBoletoJSON
	out.Cliente.ID, out.Cliente.Nome, out.Cliente.Documento = prox.ID, prox.ClienteNome, prox.CpfCnpj
	for _, c := range cotas {
		out.Cliente.Cotas = append(out.Cliente.Cotas, cotaBoletoJSON{ID: c.ID, Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao})
	}
	responderJSON(w, http.StatusOK, out)
}

type pedidoParcelas struct {
	Parcelas []struct {
		CotaID        int64  `json:"cota_id"`
		Parcela       string `json:"parcela"`
		Vencimento    string `json:"vencimento"`
		ValorCentavos int64  `json:"valor_centavos"`
		Tipo          string `json:"tipo"`
		ChargeID      *int64 `json:"charge_id"`
		TransactionID *int64 `json:"transaction_id"`
	} `json:"parcelas"`
}

// registrarParcelas grava o que o site devolveu de uma cota. Grupo/cota/versão NÃO vêm do
// worker: são lidos do cadastro pelo cota_id, que precisa ser de uma cota deste cliente.
func (s *Servidor) registrarParcelas(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cliente da rodada não encontrado")
		return
	}
	var p pedidoParcelas
	if err := lerJSON(w, r, &p, 64<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"parcelas": [...]}`)
		return
	}
	if len(p.Parcelas) == 0 {
		responderErro(w, http.StatusBadRequest, "nenhuma parcela enviada")
		return
	}
	if len(p.Parcelas) > maxParcelasPorVez {
		responderErro(w, http.StatusBadRequest, fmt.Sprintf("no máximo %d parcelas por vez", maxParcelasPorVez))
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	c, ok := s.clienteDoWorker(w, r, qtx, id, worker)
	if !ok {
		return
	}
	cotas, err := qtx.CotasDoCliente(ctx, c.ClienteID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	doCliente := map[int64]db.CotasDoClienteRow{}
	for _, q := range cotas {
		doCliente[q.ID] = q
	}

	ids := make([]int64, 0, len(p.Parcelas))
	for i, linha := range p.Parcelas {
		cota, existe := doCliente[linha.CotaID]
		if !existe {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("parcela %d: a cota %d não é deste cliente", i+1, linha.CotaID))
			return
		}
		if linha.Parcela == "" || len(linha.Parcela) > 20 {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("parcela %d: número da parcela inválido", i+1))
			return
		}
		vencimento, err := time.Parse(time.DateOnly, linha.Vencimento)
		if err != nil {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("parcela %d: vencimento precisa ser AAAA-MM-DD", i+1))
			return
		}
		if linha.ValorCentavos <= 0 {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("parcela %d: valor precisa ser maior que zero", i+1))
			return
		}
		if linha.Tipo == "" || len(linha.Tipo) > 30 {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("parcela %d: tipo inválido", i+1))
			return
		}
		parcelaID, err := qtx.CriarParcela(ctx, db.CriarParcelaParams{
			RodadaClienteID: c.ID, CotaID: cota.ID, Grupo: cota.Grupo, Cota: cota.Cota, Versao: cota.Versao,
			Parcela: linha.Parcela, Vencimento: vencimento, ValorCentavos: linha.ValorCentavos,
			Tipo: linha.Tipo, ChargeID: linha.ChargeID, TransactionID: linha.TransactionID,
		})
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		ids = append(ids, parcelaID)
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]any{"ids": ids})
}

type pedidoConcluirCliente struct {
	Status       string `json:"status"`
	ErroTipo     string `json:"erro_tipo"`
	Erro         string `json:"erro"`
	CotasNoSite  int32  `json:"cotas_no_site"`
	CotasCasadas int32  `json:"cotas_casadas"`
}

func (s *Servidor) concluirClienteBoletos(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cliente da rodada não encontrado")
		return
	}
	var p pedidoConcluirCliente
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"status": "consultado"}`)
		return
	}
	if !conclusoesCliente[p.Status] {
		responderErro(w, http.StatusBadRequest, `status precisa ser "consultado", "sem_boleto" ou "erro"`)
		return
	}
	if p.Status == "erro" && p.ErroTipo != "conhecido" && p.ErroTipo != "inesperado" {
		responderErro(w, http.StatusBadRequest, `erro_tipo precisa ser "conhecido" ou "inesperado"`)
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	c, ok := s.clienteDoWorker(w, r, qtx, id, worker)
	if !ok {
		return
	}
	var erroTipo, erro *string
	if p.Status == "erro" {
		erroTipo = &p.ErroTipo
		mensagem := textoLimitado(p.Erro, 2000)
		if mensagem == "" {
			mensagem = "erro sem mensagem"
		}
		erro = &mensagem
	}
	linhas, err := qtx.ConcluirRodadaCliente(ctx, db.ConcluirRodadaClienteParams{
		ID: c.ID, Status: p.Status, ErroTipo: erroTipo, Erro: erro,
		CotasNoSite: p.CotasNoSite, CotasCasadas: p.CotasCasadas,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "este cliente não está em andamento"})
		return
	}
	nivel, mensagem := "ok", fmt.Sprintf("%s: %d cota(s) no site, %d do cadastro.", c.ClienteNome, p.CotasNoSite, p.CotasCasadas)
	switch p.Status {
	case "sem_boleto":
		nivel, mensagem = "info", c.ClienteNome+": nenhuma parcela em aberto."
	case "erro":
		nivel, mensagem = "erro", c.ClienteNome+": "+*erro
	}
	if err := s.eventoBoleto(ctx, qtx, c.RodadaID, &c.ID, nivel, mensagem); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// enviarPdfBoleto guarda o PDF de uma parcela. O boleto é dado de pagamento de cliente: sai do
// banco em 90 dias e só é baixado pela API, com sessão (GET /arquivos/{id}).
func (s *Servidor) enviarPdfBoleto(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "parcela não encontrada")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limitePdfBoleto)
	conteudo, err := io.ReadAll(r.Body)
	if err != nil {
		responderErro(w, http.StatusRequestEntityTooLarge, "PDF maior que 10 MB")
		return
	}
	if !bytes.HasPrefix(conteudo, assinaturaPDF) {
		responderErro(w, http.StatusUnsupportedMediaType, "o arquivo precisa ser PDF")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	p, err := qtx.BuscarParcelaParaWorker(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "parcela não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if p.RodadaStatus != "em_andamento" || p.Worker == nil || *p.Worker != worker {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a rodada não está mais com este worker", Parar: true})
		return
	}
	soma := sha256.Sum256(conteudo)
	expira := s.agora().Add(retencaoPdfBoleto)
	nome := fmt.Sprintf("boleto %s-%s-%s parcela %s %s.pdf", p.Grupo, p.Cota, p.Versao, p.Parcela, p.Vencimento.Format(time.DateOnly))
	arquivoID, err := qtx.InserirArquivo(ctx, db.InserirArquivoParams{
		Tipo: "pdf", Nome: nome, ContentType: "application/pdf", Tamanho: int32(len(conteudo)),
		Sha256: hex.EncodeToString(soma[:]), Conteudo: conteudo, ExpiraEm: &expira,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if _, err := qtx.DefinirPdfDaParcela(ctx, db.DefinirPdfDaParcelaParams{ID: p.ID, PdfID: &arquivoID}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]string{"id": arquivoID})
}

type pedidoEventosBoletos struct {
	Eventos []struct {
		ClienteID *int64          `json:"cliente_id"`
		Nivel     string          `json:"nivel"`
		Mensagem  string          `json:"mensagem"`
		Dados     json.RawMessage `json:"dados"`
	} `json:"eventos"`
}

var niveisEvento = map[string]bool{"info": true, "ok": true, "aviso": true, "erro": true}

func (s *Servidor) eventosBoletos(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return
	}
	var p pedidoEventosBoletos
	if err := lerJSON(w, r, &p, 64<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"eventos": [...]}`)
		return
	}
	ctx := r.Context()
	if _, ok := s.rodadaDoWorker(w, r, s.q, id, worker); !ok {
		return
	}
	for _, e := range p.Eventos {
		if !niveisEvento[e.Nivel] {
			responderErro(w, http.StatusBadRequest, "nível precisa ser info, ok, aviso ou erro")
			return
		}
		if err := s.q.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
			RodadaID: id, RodadaClienteID: e.ClienteID, Nivel: e.Nivel,
			Mensagem: textoLimitado(e.Mensagem, 2000), Dados: objetoJSON(e.Dados),
		}); err != nil {
			erroInterno(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Servidor) finalizarRodada(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
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
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	rd, ok := s.rodadaDoWorker(w, r, qtx, id, worker)
	if !ok {
		return
	}
	totais, err := qtx.ContarClientesDaRodada(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	var erro *string
	status := "concluida"
	switch {
	case p.Erro != "":
		mensagem := textoLimitado(p.Erro, 2000)
		erro, status = &mensagem, "falhou"
	case rd.CancelamentoSolicitado:
		status = "cancelada"
	case totais.ComErro > 0:
		status = "concluida_com_erros"
	}
	if status == "cancelada" {
		if _, err := qtx.CancelarClientesPendentes(ctx, id); err != nil {
			erroInterno(w, r, err)
			return
		}
	}
	linhas, err := qtx.FinalizarRodada(ctx, db.FinalizarRodadaParams{ID: id, Worker: &worker, Status: status, Erro: erro})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a rodada não está mais com este worker", Parar: true})
		return
	}
	if err := s.eventoBoleto(ctx, qtx, id, nil, nivelDoFim(status), fmt.Sprintf("Rodada %s: %d cliente(s), %d com erro.", status, totais.Total, totais.ComErro)); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]string{"status": status})
}

func nivelDoFim(status string) string {
	switch status {
	case "concluida":
		return "ok"
	case "falhou":
		return "erro"
	}
	return "aviso"
}

// liberarRodada: o worker devolve a rodada à fila ao receber SIGTERM, sem esperar a trava vencer.
func (s *Servidor) liberarRodada(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	linhas, err := qtx.LiberarRodada(ctx, db.LiberarRodadaParams{ID: id, Worker: &worker})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "a rodada não está mais com este worker"})
		return
	}
	if _, err := qtx.DevolverClientesComTravaVencida(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := s.eventoBoleto(ctx, qtx, id, nil, "aviso", "O worker foi encerrado: a rodada voltou para a fila."); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
