package httpapi

// Serviço de boletos da Canopus (issue #48), lado de quem usa a plataforma: criar uma rodada,
// acompanhar e cancelar. Quem conversa com o Boleto Fácil é o worker, pelas rotas internas
// (interno_boletos.go) — o CPF do cliente nunca sai da API.
//
// Uma rodada é uma passada pelo cadastro: 'envio' busca os boletos em aberto e guarda o PDF;
// 'cobranca' pergunta de novo, depois, para saber o que continua em aberto. Como o site
// funciona está em docs/canopus-boleto-facil.md.

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const limiteRodadasListadas = 50

type pedidoRodada struct {
	Tipo string `json:"tipo"`
	// Mês de referência, "AAAA-MM". Vazio: o mês atual no fuso de Brasília.
	Referencia string `json:"referencia"`
}

type rodadaJSON struct {
	ID              int64      `json:"id"`
	Tipo            string     `json:"tipo"`
	Referencia      string     `json:"referencia"`
	Status          string     `json:"status"`
	Origem          string     `json:"origem"`
	CriadaPorNome   string     `json:"criada_por_nome"`
	CanceladaPor    *string    `json:"cancelada_por_nome"`
	CancelamentoPed bool       `json:"cancelamento_solicitado"`
	Erro            *string    `json:"erro"`
	CriadaEm        time.Time  `json:"criada_em"`
	IniciadaEm      *time.Time `json:"iniciada_em"`
	FinalizadaEm    *time.Time `json:"finalizada_em"`
}

type rodadaResumoJSON struct {
	ID            int64      `json:"id"`
	Tipo          string     `json:"tipo"`
	Referencia    string     `json:"referencia"`
	Status        string     `json:"status"`
	Origem        string     `json:"origem"`
	CriadaPorNome string     `json:"criada_por_nome"`
	CriadaEm      time.Time  `json:"criada_em"`
	FinalizadaEm  *time.Time `json:"finalizada_em"`
	Erro          *string    `json:"erro"`
	Total         int32      `json:"total"`
	Consultados   int32      `json:"consultados"`
	SemBoleto     int32      `json:"sem_boleto"`
	ComErro       int32      `json:"com_erro"`
	Parcelas      int32      `json:"parcelas"`
}

type rodadaClienteJSON struct {
	ID           int64      `json:"id"`
	ClienteID    int64      `json:"cliente_id"`
	Ordem        int32      `json:"ordem"`
	ClienteNome  string     `json:"cliente_nome"`
	Status       string     `json:"status"`
	ErroTipo     *string    `json:"erro_tipo"`
	Erro         *string    `json:"erro"`
	CotasNoSite  int32      `json:"cotas_no_site"`
	CotasCasadas int32      `json:"cotas_casadas"`
	Tentativas   int32      `json:"tentativas"`
	IniciadoEm   *time.Time `json:"iniciado_em"`
	FinalizadoEm *time.Time `json:"finalizado_em"`
}

type parcelaJSON struct {
	ID              int64     `json:"id"`
	RodadaClienteID int64     `json:"rodada_cliente_id"`
	CotaID          int64     `json:"cota_id"`
	Grupo           string    `json:"grupo"`
	Cota            string    `json:"cota"`
	Versao          string    `json:"versao"`
	Parcela         string    `json:"parcela"`
	Vencimento      string    `json:"vencimento"`
	ValorCentavos   int64     `json:"valor_centavos"`
	Tipo            string    `json:"tipo"`
	PdfID           *string   `json:"pdf_id"`
	CriadaEm        time.Time `json:"criada_em"`
}

// mesDeReferencia normaliza "AAAA-MM" (ou vazio = mês atual) no dia 1 do mês.
func (s *Servidor) mesDeReferencia(bruto string) (time.Time, error) {
	if bruto == "" {
		agora := s.agora().In(fusoCanopus)
		return time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	m, err := time.Parse("2006-01", bruto)
	if err != nil {
		return time.Time{}, fmt.Errorf("referência inválida: use AAAA-MM (recebido %q)", bruto)
	}
	return time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

// criarRodada põe na fila uma passada pelo cadastro, com todos os clientes que têm documento.
func (s *Servidor) criarRodada(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoRodada
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"tipo": "envio"} ou {"tipo": "cobranca"}`)
		return
	}
	if p.Tipo != "envio" && p.Tipo != "cobranca" {
		responderErro(w, http.StatusBadRequest, `tipo precisa ser "envio" ou "cobranca"`)
		return
	}
	referencia, err := s.mesDeReferencia(p.Referencia)
	if err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()

	// Cobrança sem envio concluído no mês não tem com o que comparar: cobraria gente que nunca
	// recebeu o boleto da plataforma. Melhor recusar e deixar claro o que fazer.
	if p.Tipo == "cobranca" {
		base, err := s.q.ParcelasDoEnvio(ctx, referencia)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		if len(base) == 0 {
			responderErro(w, http.StatusConflict,
				"não há rodada de envio concluída em "+referencia.Format("01/2006")+
					": rode o envio antes, senão a cobrança não tem com o que comparar")
			return
		}
	}

	clientes, err := s.q.ClientesParaBoletos(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if len(clientes) == 0 {
		responderErro(w, http.StatusConflict,
			"nenhum cliente com CPF/CNPJ e cota cadastrados: o Boleto Fácil só responde por documento")
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	rodada, err := qtx.CriarRodada(ctx, db.CriarRodadaParams{
		Tipo: p.Tipo, Referencia: referencia, Origem: "manual", CriadaPor: u.ID,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	for i, c := range clientes {
		if _, err := qtx.CriarRodadaCliente(ctx, db.CriarRodadaClienteParams{
			RodadaID: rodada.ID, ClienteID: c.ID, Ordem: int32(i + 1), ClienteNome: c.Nome,
		}); err != nil {
			erroInterno(w, r, err)
			return
		}
	}
	if err := qtx.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
		RodadaID: rodada.ID, Nivel: "info", Dados: []byte("{}"),
		Mensagem: fmt.Sprintf("Rodada de %s criada por %s com %d cliente(s).", p.Tipo, u.Nome, len(clientes)),
	}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}

	s.auditar(ctx, r, &u.ID, "boletos_rodada_criada", "boletos_rodada", idTexto(rodada.ID),
		map[string]any{"tipo": p.Tipo, "referencia": referencia.Format("2006-01"), "clientes": len(clientes)})
	s.responderRodada(w, r, rodada.ID, http.StatusCreated)
}

func (s *Servidor) listarRodadas(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	rows, err := s.q.ListarRodadas(r.Context(), limiteRodadasListadas)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	out := make([]rodadaResumoJSON, 0, len(rows))
	for _, x := range rows {
		out = append(out, rodadaResumoJSON{
			ID: x.ID, Tipo: x.Tipo, Referencia: x.Referencia.Format("2006-01"), Status: x.Status,
			Origem: x.Origem, CriadaPorNome: x.CriadaPorNome, CriadaEm: x.CriadaEm,
			FinalizadaEm: x.FinalizadaEm, Erro: x.Erro, Total: x.Total, Consultados: x.Consultados,
			SemBoleto: x.SemBoleto, ComErro: x.ComErro, Parcelas: x.Parcelas,
		})
	}
	responderJSON(w, http.StatusOK, map[string]any{"rodadas": out})
}

func (s *Servidor) buscarRodada(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return
	}
	s.responderRodada(w, r, id, http.StatusOK)
}

func (s *Servidor) responderRodada(w http.ResponseWriter, r *http.Request, id int64, status int) {
	ctx := r.Context()
	rd, err := s.q.BuscarRodada(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "rodada não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	clientes, err := s.q.ListarRodadaClientes(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	parcelas, err := s.q.ListarParcelasDaRodada(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	eventos, err := s.q.ListarEventosDaRodada(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}

	saidaClientes := make([]rodadaClienteJSON, 0, len(clientes))
	for _, c := range clientes {
		saidaClientes = append(saidaClientes, rodadaClienteJSON{
			ID: c.ID, ClienteID: c.ClienteID, Ordem: c.Ordem, ClienteNome: c.ClienteNome,
			Status: c.Status, ErroTipo: c.ErroTipo, Erro: c.Erro, CotasNoSite: c.CotasNoSite,
			CotasCasadas: c.CotasCasadas, Tentativas: c.Tentativas,
			IniciadoEm: c.IniciadoEm, FinalizadoEm: c.FinalizadoEm,
		})
	}
	saidaParcelas := make([]parcelaJSON, 0, len(parcelas))
	for _, p := range parcelas {
		saidaParcelas = append(saidaParcelas, parcelaJSON{
			ID: p.ID, RodadaClienteID: p.RodadaClienteID, CotaID: p.CotaID, Grupo: p.Grupo,
			Cota: p.Cota, Versao: p.Versao, Parcela: p.Parcela,
			Vencimento: p.Vencimento.Format(time.DateOnly), ValorCentavos: p.ValorCentavos,
			Tipo: p.Tipo, PdfID: p.PdfID, CriadaEm: p.CriadaEm,
		})
	}
	saidaEventos := make([]eventoBoletoJSON, 0, len(eventos))
	for _, e := range eventos {
		saidaEventos = append(saidaEventos, eventoBoletoJSON{
			ID: e.ID, RodadaClienteID: e.RodadaClienteID, Nivel: e.Nivel,
			Mensagem: e.Mensagem, CriadoEm: e.CriadoEm,
		})
	}
	responderJSON(w, status, map[string]any{
		"rodada": rodadaJSON{
			ID: rd.ID, Tipo: rd.Tipo, Referencia: rd.Referencia.Format("2006-01"), Status: rd.Status,
			Origem: rd.Origem, CriadaPorNome: rd.CriadaPorNome, CanceladaPor: rd.CanceladaPorNome,
			CancelamentoPed: rd.CancelamentoSolicitado, Erro: rd.Erro, CriadaEm: rd.CriadaEm,
			IniciadaEm: rd.IniciadaEm, FinalizadaEm: rd.FinalizadaEm,
		},
		"clientes": saidaClientes,
		"parcelas": saidaParcelas,
		"eventos":  saidaEventos,
	})
}

type eventoBoletoJSON struct {
	ID              int64     `json:"id"`
	RodadaClienteID *int64    `json:"rodada_cliente_id"`
	Nivel           string    `json:"nivel"`
	Mensagem        string    `json:"mensagem"`
	CriadoEm        time.Time `json:"criado_em"`
}

// cancelarRodada: na fila cancela na hora; em andamento, o worker termina o cliente atual e
// não começa o próximo (nada aqui é irreversível, mas o site tem limite de taxa).
func (s *Servidor) cancelarRodada(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
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

	linhas, err := qtx.CancelarRodada(ctx, db.CancelarRodadaParams{ID: id, CanceladaPor: &u.ID})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderErro(w, http.StatusConflict, "a rodada já terminou ou não existe")
		return
	}
	if _, err := qtx.CancelarClientesPendentes(ctx, id); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
		RodadaID: id, Nivel: "aviso", Dados: []byte("{}"),
		Mensagem: "Cancelamento pedido por " + u.Nome + ".",
	}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "boletos_rodada_cancelada", "boletos_rodada", idTexto(id), nil)
	s.responderRodada(w, r, id, http.StatusOK)
}
