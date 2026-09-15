package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

type clienteResumoJSON struct {
	ID          int64   `json:"id"`
	Nome        string  `json:"nome"`
	Telefone    *string `json:"telefone"`
	Email       *string `json:"email"`
	TotalCotas  int32   `json:"total_cotas"`
	CotasAtivas int32   `json:"cotas_ativas"`
}

type clienteJSON struct {
	ID           int64     `json:"id"`
	Nome         string    `json:"nome"`
	Telefone     *string   `json:"telefone"`
	Email        *string   `json:"email"`
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
	rows, err := s.q.ListarClientes(r.Context(), busca(r))
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	out := make([]clienteResumoJSON, 0, len(rows))
	for _, c := range rows {
		out = append(out, clienteResumoJSON{c.ID, c.Nome, c.Telefone, c.Email, c.TotalCotas, c.CotasAtivas})
	}
	responderJSON(w, http.StatusOK, map[string]any{"clientes": out})
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
	responderJSON(w, http.StatusOK, map[string]any{
		"cliente": clienteJSON{c.ID, c.Nome, c.Telefone, c.Email, c.Origem, c.CriadoEm, c.AtualizadoEm},
		"cotas":   cotasParaJSON(cotas),
	})
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
		})
	}
	return out
}

func idTexto(id int64) string { return strconv.FormatInt(id, 10) }
