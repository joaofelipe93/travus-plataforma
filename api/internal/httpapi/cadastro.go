package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/servicos"
)

type clienteResumoJSON struct {
	ID       int64   `json:"id"`
	Nome     string  `json:"nome"`
	Telefone *string `json:"telefone"`
	Email    *string `json:"email"`
	// Documento do cliente (só dígitos), usado pelo Boleto Fácil do Canopus.
	CpfCnpj     *string `json:"cpf_cnpj"`
	TotalCotas  int32   `json:"total_cotas"`
	CotasAtivas int32   `json:"cotas_ativas"`
	// Serviços em que o cliente está ativo (migração 00018), para a lista marcar cada um.
	Servicos []string `json:"servicos"`
}

// servicoDoClienteJSON é um serviço do catálogo com a situação daquele cliente nele. A API
// manda sempre o catálogo inteiro: a tela desenha um interruptor por serviço, inclusive os
// que nunca foram tocados para aquele cliente.
type servicoDoClienteJSON struct {
	ID              string     `json:"id"`
	Nome            string     `json:"nome"`
	Descricao       string     `json:"descricao"`
	Ativo           bool       `json:"ativo"`
	AtualizadoEm    *time.Time `json:"atualizado_em"`
	AlteradoPorNome *string    `json:"alterado_por_nome"`
}

type clienteJSON struct {
	ID           int64     `json:"id"`
	Nome         string    `json:"nome"`
	Telefone     *string   `json:"telefone"`
	Email        *string   `json:"email"`
	CpfCnpj      *string   `json:"cpf_cnpj"`
	Origem       string    `json:"origem"`
	CriadoEm     time.Time `json:"criado_em"`
	AtualizadoEm time.Time `json:"atualizado_em"`
}

type cotaJSON struct {
	ID               int64     `json:"id"`
	ClienteID        int64     `json:"cliente_id"`
	ClienteNome      string    `json:"cliente_nome"`
	Administradora   string    `json:"administradora"`
	Grupo            string    `json:"grupo"`
	Cota             string    `json:"cota"`
	Versao           string    `json:"versao"`
	TipoConsorcio    *string   `json:"tipo_consorcio"`
	ModalidadePadrao string    `json:"modalidade_padrao"`
	Ativa            bool      `json:"ativa"`
	AtualizadoEm     time.Time `json:"atualizado_em"`
	// Campos do cadastro (antes soltos em dados_planilha). Contratação em AAAA-MM-DD.
	Vendedor          *string `json:"vendedor"`
	FormaPagamento    *string `json:"forma_pagamento"`
	VencimentoParcela *int16  `json:"vencimento_parcela"`
	DiaAssembleia     *int16  `json:"dia_assembleia"`
	Contratacao       *string `json:"contratacao"`
}

// padraoLike escapa % e _ digitados na busca.
var escaparLike = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func busca(r *http.Request) *string {
	b := strings.TrimSpace(r.URL.Query().Get("busca"))
	if b == "" {
		return nil
	}
	b = escaparLike.Replace(b)
	return &b
}

func (s *Servidor) listarClientes(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	// ?servico=canopus: só quem está ativo naquele serviço. Sem ele, a carteira inteira.
	var servico *string
	if v := strings.TrimSpace(r.URL.Query().Get("servico")); v != "" {
		if !servicos.Valido(v) {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("serviço desconhecido: %s", v))
			return
		}
		servico = &v
	}
	rows, err := s.q.ListarClientes(r.Context(), db.ListarClientesParams{Busca: busca(r), Servico: servico})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	out := make([]clienteResumoJSON, 0, len(rows))
	for _, c := range rows {
		servicosAtivos := c.Servicos
		if servicosAtivos == nil {
			servicosAtivos = []string{}
		}
		out = append(out, clienteResumoJSON{c.ID, c.Nome, c.Telefone, c.Email, c.CpfCnpj, c.TotalCotas, c.CotasAtivas, servicosAtivos})
	}
	responderJSON(w, http.StatusOK, map[string]any{"clientes": out})
}

// listarServicos devolve o catálogo de serviços que agem sobre um cliente. A tela usa para
// montar os interruptores sem repetir a lista no código dela.
func (s *Servidor) listarServicos(w http.ResponseWriter, _ *http.Request, _ *UsuarioSessao) {
	responderJSON(w, http.StatusOK, map[string]any{"servicos": servicos.Todos})
}

// servicosDoCliente junta o catálogo com o que está gravado para aquele cliente.
func (s *Servidor) servicosDoCliente(ctx context.Context, clienteID int64) ([]servicoDoClienteJSON, error) {
	rows, err := s.q.ListarServicosDoCliente(ctx, clienteID)
	if err != nil {
		return nil, err
	}
	gravados := make(map[string]db.ListarServicosDoClienteRow, len(rows))
	for _, row := range rows {
		gravados[row.Servico] = row
	}
	out := make([]servicoDoClienteJSON, 0, len(servicos.Todos))
	for _, cat := range servicos.Todos {
		item := servicoDoClienteJSON{ID: cat.ID, Nome: cat.Nome, Descricao: cat.Descricao}
		if row, ok := gravados[cat.ID]; ok {
			item.Ativo = row.Ativo
			atualizado := row.AtualizadoEm
			item.AtualizadoEm = &atualizado
			item.AlteradoPorNome = row.AlteradoPorNome
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Servidor) buscarCliente(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	c, err := s.q.BuscarCliente(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	cotas, err := s.q.ListarCotas(r.Context(), db.ListarCotasParams{ClienteID: &id})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	lancesRows, err := s.q.ListarLancesDoCliente(r.Context(), id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	lances := make([]lanceClienteJSON, 0, len(lancesRows))
	for _, l := range lancesRows {
		var assembleia *string
		if l.AssembleiaData != nil {
			texto := l.AssembleiaData.Format("02/01/2006")
			assembleia = &texto
		}
		lances = append(lances, lanceClienteJSON{
			ID: l.ID, CotaID: l.CotaID, Grupo: l.Grupo, Cota: l.Cota, Versao: l.Versao, Origem: l.Origem, Protocolo: l.Protocolo,
			AssembleiaData: assembleia, AssembleiaNumero: l.AssembleiaNumero, Modalidade: l.Modalidade, Percentual: l.Percentual,
			ParcelasEmAtraso: l.ParcelasEmAtraso, LanceExistenteAutorizado: l.LanceExistenteAutorizado, PdfID: l.PdfID,
			DriveStatus: l.DriveStatus, DriveLink: l.DriveLink, DriveErro: l.DriveErro, RegistradoEm: l.RegistradoEm, ExecucaoID: l.ExecucaoID,
		})
	}
	servicosDoCliente, err := s.servicosDoCliente(r.Context(), id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"cliente":  clienteJSON{c.ID, c.Nome, c.Telefone, c.Email, c.CpfCnpj, c.Origem, c.CriadoEm, c.AtualizadoEm},
		"cotas":    cotasParaJSON(cotas),
		"lances":   lances,
		"servicos": servicosDoCliente,
	})
}

type lanceClienteJSON struct {
	ID                       int64      `json:"id"`
	CotaID                   int64      `json:"cota_id"`
	Grupo                    string     `json:"grupo"`
	Cota                     string     `json:"cota"`
	Versao                   string     `json:"versao"`
	Origem                   string     `json:"origem"`
	Protocolo                string     `json:"protocolo"`
	AssembleiaData           *string    `json:"assembleia_data"`
	AssembleiaNumero         *string    `json:"assembleia_numero"`
	Modalidade               string     `json:"modalidade"`
	Percentual               *string    `json:"percentual"`
	ParcelasEmAtraso         bool       `json:"parcelas_em_atraso"`
	LanceExistenteAutorizado bool       `json:"lance_existente_autorizado"`
	PdfID                    *string    `json:"pdf_id"`
	DriveStatus              string     `json:"drive_status"`
	DriveLink                *string    `json:"drive_link"`
	DriveErro                *string    `json:"drive_erro"`
	RegistradoEm             *time.Time `json:"registrado_em"`
	ExecucaoID               *int64     `json:"execucao_id"`
}

func (s *Servidor) listarCotas(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	params := db.ListarCotasParams{Busca: busca(r)}
	if g := strings.TrimSpace(r.URL.Query().Get("grupo")); g != "" {
		params.Grupo = &g
	}
	switch a := r.URL.Query().Get("ativa"); a {
	case "":
	case "true", "false":
		v := a == "true"
		params.Ativa = &v
	default:
		responderErro(w, http.StatusBadRequest, `filtro "ativa" deve ser true ou false`)
		return
	}
	rows, err := s.q.ListarCotas(r.Context(), params)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{"cotas": cotasParaJSON(rows)})
}

func (s *Servidor) listarGrupos(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	grupos, err := s.q.ListarGrupos(r.Context())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if grupos == nil {
		grupos = []string{}
	}
	responderJSON(w, http.StatusOK, map[string]any{"grupos": grupos})
}

type pedidoCota struct {
	Ativa *bool `json:"ativa"`
}

func (s *Servidor) definirCotaAtiva(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	var p pedidoCota
	if err := lerJSON(w, r, &p, 1<<10); err != nil || p.Ativa == nil {
		responderErro(w, http.StatusBadRequest, `envie {"ativa": true} ou {"ativa": false}`)
		return
	}
	c, err := s.q.DefinirCotaAtiva(r.Context(), db.DefinirCotaAtivaParams{ID: id, Ativa: *p.Ativa})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	acao := "cota_desativada"
	if c.Ativa {
		acao = "cota_ativada"
	}
	s.auditar(r.Context(), r, &u.ID, acao, "cota", idTexto(c.ID), map[string]any{"cota": fmt.Sprintf("%s-%s-%s", c.Grupo, c.Cota, c.Versao)})
	responderJSON(w, http.StatusOK, map[string]any{"id": c.ID, "ativa": c.Ativa})
}

func cotasParaJSON(rows []db.ListarCotasRow) []cotaJSON {
	out := make([]cotaJSON, 0, len(rows))
	for _, c := range rows {
		out = append(out, cotaJSON{
			ID: c.ID, ClienteID: c.ClienteID, ClienteNome: c.ClienteNome, Administradora: c.Administradora,
			Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao, TipoConsorcio: c.TipoConsorcio,
			ModalidadePadrao: c.ModalidadePadrao, Ativa: c.Ativa, AtualizadoEm: c.AtualizadoEm,
			Vendedor: c.Vendedor, FormaPagamento: c.FormaPagamento, VencimentoParcela: c.VencimentoParcela,
			DiaAssembleia: c.DiaAssembleia, Contratacao: dataOuNil(c.Contratacao),
		})
	}
	return out
}

func idTexto(id int64) string { return strconv.FormatInt(id, 10) }
