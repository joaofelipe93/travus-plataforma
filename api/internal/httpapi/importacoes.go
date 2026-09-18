package httpapi

// Histórico das importações de planilha, só para consulta: o cadastro do Canopus virou CRM
// (crm.go) e importar planilha saiu da plataforma. As cotas importadas apontam para estes
// registros, por isso eles continuam no banco.

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/importacao"
)

type importacaoJSON struct {
	ID            int64             `json:"id"`
	Status        string            `json:"status"`
	ArquivoNome   string            `json:"arquivo_nome"`
	CriadaPorNome string            `json:"criada_por_nome"`
	CriadaEm      time.Time         `json:"criada_em"`
	FinalizadaEm  *time.Time        `json:"finalizada_em"`
	Previa        importacao.Previa `json:"previa"`
}

func (s *Servidor) buscarImportacao(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "importação não encontrada")
		return
	}
	imp, err := s.q.BuscarImportacao(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "importação não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	var previa importacao.Previa
	if err := json.Unmarshal(imp.Previa, &previa); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, importacaoJSON{
		ID: imp.ID, Status: imp.Status, ArquivoNome: imp.ArquivoNome, CriadaPorNome: imp.CriadaPorNome,
		CriadaEm: imp.CriadaEm, FinalizadaEm: imp.FinalizadaEm, Previa: previa,
	})
}

func (s *Servidor) listarImportacoes(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	rows, err := s.q.ListarImportacoes(r.Context())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	type item struct {
		ID            int64      `json:"id"`
		Status        string     `json:"status"`
		ArquivoNome   string     `json:"arquivo_nome"`
		CriadaPorNome string     `json:"criada_por_nome"`
		Totais        any        `json:"totais"`
		CriadaEm      time.Time  `json:"criada_em"`
		FinalizadaEm  *time.Time `json:"finalizada_em"`
	}
	out := make([]item, 0, len(rows))
	for _, i := range rows {
		out = append(out, item{i.ID, i.Status, i.ArquivoNome, i.CriadaPorNome, i.Totais, i.CriadaEm, i.FinalizadaEm})
	}
	responderJSON(w, http.StatusOK, map[string]any{"importacoes": out})
}
