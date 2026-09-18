package httpapi

// Cadastro do Canopus (CRM): clientes e cotas digitados pelo admin/operador. Substituiu a
// importação de planilha — as rotas de importar saíram; a tabela importacoes e o que já foi
// importado continuam no banco. As regras do que pode ser digitado estão em internal/cadastro.

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cadastro"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const (
	limiteCorpoCliente = 1 << 16 // cliente com muitas cotas de uma vez
	limiteCorpoCota    = 1 << 13
)

type pedidoCliente struct {
	Nome     string               `json:"nome"`
	Telefone string               `json:"telefone"`
	Email    string               `json:"email"`
	Cotas    []pedidoCotaCadastro `json:"cotas"`
}

type pedidoCotaCadastro struct {
	ClienteID         *int64 `json:"cliente_id"`
	Administradora    string `json:"administradora"`
	Grupo             string `json:"grupo"`
	Cota              string `json:"cota"`
	Versao            string `json:"versao"`
	TipoConsorcio     string `json:"tipo_consorcio"`
	ModalidadePadrao  string `json:"modalidade_padrao"`
	Ativa             *bool  `json:"ativa"`
	Vendedor          string `json:"vendedor"`
	FormaPagamento    string `json:"forma_pagamento"`
	VencimentoParcela *int   `json:"vencimento_parcela"`
	DiaAssembleia     *int   `json:"dia_assembleia"`
	Contratacao       string `json:"contratacao"`
}

// cotaValidada é o pedido já normalizado, pronto para o banco.
type cotaValidada struct {
	Administradora    string
	Grupo             string
	Cota              string
	Versao            string
	TipoConsorcio     *string
	ModalidadePadrao  string
	Ativa             bool
	Vendedor          *string
	FormaPagamento    *string
	VencimentoParcela *int16
	DiaAssembleia     *int16
	Contratacao       *time.Time
}

func (c cotaValidada) tag() string { return c.Grupo + "-" + c.Cota + "-" + c.Versao }

func validarCota(p pedidoCotaCadastro) (cotaValidada, error) {
	var v cotaValidada
	var err error
	if v.Administradora, err = cadastro.Administradora(p.Administradora); err != nil {
		return v, err
	}
	if v.Grupo, err = cadastro.Grupo(p.Grupo); err != nil {
		return v, err
	}
	if v.Cota, err = cadastro.Cota(p.Cota); err != nil {
		return v, err
	}
	if v.Versao, err = cadastro.Versao(p.Versao); err != nil {
		return v, err
	}
	if v.TipoConsorcio, err = cadastro.TextoMaiusculo("tipo de consórcio", p.TipoConsorcio); err != nil {
		return v, err
	}
	if v.ModalidadePadrao, err = cadastro.Modalidade(p.ModalidadePadrao); err != nil {
		return v, err
	}
	if v.Vendedor, err = cadastro.Texto("vendedor", p.Vendedor); err != nil {
		return v, err
	}
	if v.FormaPagamento, err = cadastro.TextoMaiusculo("forma de pagamento", p.FormaPagamento); err != nil {
		return v, err
	}
	if v.VencimentoParcela, err = cadastro.DiaDoMes("vencimento da parcela", p.VencimentoParcela); err != nil {
		return v, err
	}
	if v.DiaAssembleia, err = cadastro.DiaDoMes("dia da assembleia", p.DiaAssembleia); err != nil {
		return v, err
	}
	if v.Contratacao, err = cadastro.Data("data de contratação", p.Contratacao); err != nil {
		return v, err
	}
	v.Ativa = p.Ativa == nil || *p.Ativa
	return v, nil
}

// responderErroCadastro traduz o erro de validação (400) e o de chave repetida (409).
func responderErroCadastro(w http.ResponseWriter, r *http.Request, err error) {
	var ev cadastro.Erro
	if errors.As(err, &ev) {
		responderErro(w, http.StatusBadRequest, ev.Mensagem)
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		mensagem := "esse cadastro já existe"
		switch pgErr.ConstraintName {
		case "clientes_nome_normalizado_key":
			mensagem = "já existe um cliente com esse nome"
		case "cotas_administradora_grupo_cota_versao_key":
			mensagem = "essa cota já está cadastrada (administradora, grupo, cota e versão se repetem)"
		}
		responderErro(w, http.StatusConflict, mensagem)
		return
	}
	erroInterno(w, r, err)
}

func (s *Servidor) criarCliente(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoCliente
	if err := lerJSON(w, r, &p, limiteCorpoCliente); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	nome, normalizado, err := cadastro.Nome(p.Nome)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	telefone, err := cadastro.Texto("telefone", p.Telefone)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	email, err := cadastro.Email(p.Email)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	cotas := make([]cotaValidada, 0, len(p.Cotas))
	vistas := map[string]bool{}
	for i, pc := range p.Cotas {
		if pc.ClienteID != nil {
			responderErro(w, http.StatusBadRequest, "as cotas enviadas com o cliente novo não levam cliente_id")
			return
		}
		c, err := validarCota(pc)
		if err != nil {
			responderErroCadastro(w, r, fmt.Errorf("cota %d: %w", i+1, err))
			return
		}
		chave := c.Administradora + "|" + c.tag()
		if vistas[chave] {
			responderErro(w, http.StatusBadRequest, fmt.Sprintf("a cota %s aparece duas vezes no formulário", c.tag()))
			return
		}
		vistas[chave] = true
		cotas = append(cotas, c)
	}

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	cliente, err := qtx.CriarClienteCadastro(ctx, db.CriarClienteCadastroParams{
		Nome: nome, NomeNormalizado: normalizado, Telefone: telefone, Email: email,
	})
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	ids := make([]int64, 0, len(cotas))
	for _, c := range cotas {
		id, err := qtx.CriarCota(ctx, paramsCriarCota(cliente.ID, c))
		if err != nil {
			responderErroCadastro(w, r, err)
			return
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}

	s.auditar(ctx, r, &u.ID, "cliente_criado", "cliente", idTexto(cliente.ID),
		map[string]any{"nome": cliente.Nome, "cotas": len(ids)})
	s.responderCliente(w, r, cliente.ID, http.StatusCreated)
}

func (s *Servidor) atualizarCliente(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	var p pedidoCliente
	if err := lerJSON(w, r, &p, limiteCorpoCota); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(p.Cotas) > 0 {
		responderErro(w, http.StatusBadRequest, "as cotas são cadastradas uma a uma em /cotas")
		return
	}
	nome, normalizado, err := cadastro.Nome(p.Nome)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	telefone, err := cadastro.Texto("telefone", p.Telefone)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	email, err := cadastro.Email(p.Email)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	c, err := s.q.AtualizarCliente(r.Context(), db.AtualizarClienteParams{
		ID: id, Nome: nome, NomeNormalizado: normalizado, Telefone: telefone, Email: email,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "cliente_editado", "cliente", idTexto(c.ID), map[string]any{"nome": c.Nome})
	s.responderCliente(w, r, c.ID, http.StatusOK)
}

func (s *Servidor) excluirCliente(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	ctx := r.Context()
	c, err := s.q.BuscarCliente(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	cotas, err := s.q.ContarCotasDoCliente(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if cotas > 0 {
		responderErro(w, http.StatusConflict,
			fmt.Sprintf("o cliente ainda tem %d cota(s): exclua ou passe as cotas para outro cliente antes", cotas))
		return
	}
	linhas, err := s.q.ExcluirCliente(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	}
	s.auditar(ctx, r, &u.ID, "cliente_excluido", "cliente", idTexto(id), map[string]any{"nome": c.Nome})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Servidor) criarCota(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoCotaCadastro
	if err := lerJSON(w, r, &p, limiteCorpoCota); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.ClienteID == nil || *p.ClienteID <= 0 {
		responderErro(w, http.StatusBadRequest, "informe o cliente da cota (cliente_id)")
		return
	}
	v, err := validarCota(p)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	ctx := r.Context()
	if _, err := s.q.BuscarCliente(ctx, *p.ClienteID); errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cliente não encontrado")
		return
	} else if err != nil {
		erroInterno(w, r, err)
		return
	}
	id, err := s.q.CriarCota(ctx, paramsCriarCota(*p.ClienteID, v))
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "cota_criada", "cota", idTexto(id),
		map[string]any{"cota": v.tag(), "cliente_id": *p.ClienteID})
	s.responderCota(w, r, id, http.StatusCreated)
}

func (s *Servidor) atualizarCota(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	var p pedidoCotaCadastro
	if err := lerJSON(w, r, &p, limiteCorpoCota); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.ClienteID == nil || *p.ClienteID <= 0 {
		responderErro(w, http.StatusBadRequest, "informe o cliente da cota (cliente_id)")
		return
	}
	v, err := validarCota(p)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	ctx := r.Context()
	atual, err := s.q.BuscarCota(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	// Cota com histórico não muda de identidade nem de dono: os lances e as execuções
	// registrados apontam para ela. O resto (modalidade, contatos da planilha) pode mudar.
	mudouIdentidade := atual.Administradora != v.Administradora || atual.Grupo != v.Grupo ||
		atual.Cota != v.Cota || atual.Versao != v.Versao || atual.ClienteID != *p.ClienteID
	if mudouIdentidade {
		uso, err := s.q.ContarUsoDaCota(ctx, id)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		if uso.Lances > 0 || uso.Execucoes > 0 {
			responderErro(w, http.StatusConflict, fmt.Sprintf(
				"a cota %s já tem histórico (%d lance(s) e %d execução(ões)): dá para mudar os dados, mas não a administradora, o grupo, a cota, a versão nem o cliente",
				atual.Grupo+"-"+atual.Cota+"-"+atual.Versao, uso.Lances, uso.Execucoes))
			return
		}
		if _, err := s.q.BuscarCliente(ctx, *p.ClienteID); errors.Is(err, pgx.ErrNoRows) {
			responderErro(w, http.StatusNotFound, "cliente não encontrado")
			return
		} else if err != nil {
			erroInterno(w, r, err)
			return
		}
	}
	if _, err := s.q.AtualizarCota(ctx, db.AtualizarCotaParams{
		ID: id, ClienteID: *p.ClienteID, Administradora: v.Administradora, Grupo: v.Grupo,
		Cota: v.Cota, Versao: v.Versao, TipoConsorcio: v.TipoConsorcio,
		ModalidadePadrao: v.ModalidadePadrao, Ativa: v.Ativa, Vendedor: v.Vendedor,
		FormaPagamento: v.FormaPagamento, VencimentoParcela: v.VencimentoParcela,
		DiaAssembleia: v.DiaAssembleia, Contratacao: v.Contratacao,
	}); err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "cota_editada", "cota", idTexto(id), map[string]any{"cota": v.tag()})
	s.responderCota(w, r, id, http.StatusOK)
}

func (s *Servidor) excluirCota(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	ctx := r.Context()
	atual, err := s.q.BuscarCota(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	uso, err := s.q.ContarUsoDaCota(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if uso.Lances > 0 || uso.Execucoes > 0 {
		responderErro(w, http.StatusConflict, fmt.Sprintf(
			"a cota %s tem histórico (%d lance(s) e %d execução(ões)) e não pode ser excluída: desative para ela parar de entrar nas execuções",
			atual.Grupo+"-"+atual.Cota+"-"+atual.Versao, uso.Lances, uso.Execucoes))
		return
	}
	linhas, err := s.q.ExcluirCota(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	s.auditar(ctx, r, &u.ID, "cota_excluida", "cota", idTexto(id),
		map[string]any{"cota": atual.Grupo + "-" + atual.Cota + "-" + atual.Versao, "cliente_id": atual.ClienteID})
	w.WriteHeader(http.StatusNoContent)
}

func paramsCriarCota(clienteID int64, c cotaValidada) db.CriarCotaParams {
	return db.CriarCotaParams{
		ClienteID: clienteID, Administradora: c.Administradora, Grupo: c.Grupo, Cota: c.Cota,
		Versao: c.Versao, TipoConsorcio: c.TipoConsorcio, ModalidadePadrao: c.ModalidadePadrao,
		Ativa: c.Ativa, Vendedor: c.Vendedor, FormaPagamento: c.FormaPagamento,
		VencimentoParcela: c.VencimentoParcela, DiaAssembleia: c.DiaAssembleia, Contratacao: c.Contratacao,
	}
}

// responderCliente devolve o cliente com as cotas, no mesmo formato do GET /clientes/{id}.
func (s *Servidor) responderCliente(w http.ResponseWriter, r *http.Request, id int64, status int) {
	ctx := r.Context()
	c, err := s.q.BuscarCliente(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	cotas, err := s.q.ListarCotas(ctx, db.ListarCotasParams{ClienteID: &id})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, status, map[string]any{
		"cliente": clienteJSON{c.ID, c.Nome, c.Telefone, c.Email, c.Origem, c.CriadoEm, c.AtualizadoEm},
		"cotas":   cotasParaJSON(cotas),
	})
}

func (s *Servidor) responderCota(w http.ResponseWriter, r *http.Request, id int64, status int) {
	c, err := s.q.BuscarCota(r.Context(), id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, status, map[string]any{"cota": cotaJSON{
		ID: c.ID, ClienteID: c.ClienteID, ClienteNome: c.ClienteNome, Administradora: c.Administradora,
		Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao, TipoConsorcio: c.TipoConsorcio,
		ModalidadePadrao: c.ModalidadePadrao, Ativa: c.Ativa, AtualizadoEm: c.AtualizadoEm,
		Vendedor: c.Vendedor, FormaPagamento: c.FormaPagamento, VencimentoParcela: c.VencimentoParcela,
		DiaAssembleia: c.DiaAssembleia, Contratacao: dataOuNil(c.Contratacao),
	}})
}

// dataOuNil formata a data como AAAA-MM-DD, o formato que o campo de data da web usa.
func dataOuNil(d *time.Time) *string {
	if d == nil {
		return nil
	}
	s := d.Format(time.DateOnly)
	return &s
}
