package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/importacao"
)

const limiteArquivo = 2 << 20 // 2 MB

type importacaoJSON struct {
	ID            int64             `json:"id"`
	Status        string            `json:"status"`
	ArquivoNome   string            `json:"arquivo_nome"`
	CriadaPorNome string            `json:"criada_por_nome"`
	CriadaEm      time.Time         `json:"criada_em"`
	FinalizadaEm  *time.Time        `json:"finalizada_em"`
	Previa        importacao.Previa `json:"previa"`
}

func (s *Servidor) criarImportacao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	r.Body = http.MaxBytesReader(w, r.Body, limiteArquivo+(64<<10))
	arquivo, cabecalho, err := r.FormFile("arquivo")
	if err != nil {
		var grande *http.MaxBytesError
		if errors.As(err, &grande) {
			responderErro(w, http.StatusRequestEntityTooLarge, "a planilha passa de 2 MB")
			return
		}
		responderErro(w, http.StatusBadRequest, `envie a planilha (CSV) no campo "arquivo"`)
		return
	}
	defer arquivo.Close()
	conteudo, err := io.ReadAll(io.LimitReader(arquivo, limiteArquivo+1))
	if err != nil {
		responderErro(w, http.StatusBadRequest, "não foi possível ler a planilha")
		return
	}
	if len(conteudo) > limiteArquivo {
		responderErro(w, http.StatusRequestEntityTooLarge, "a planilha passa de 2 MB")
		return
	}

	linhas, errosLeitura, err := importacao.LerPlanilha(conteudo)
	if err != nil {
		responderErro(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	ctx := r.Context()
	estado, err := carregarEstado(ctx, s.q)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	plano := importacao.Planejar(linhas, errosLeitura, estado)

	linhasJSON, _ := json.Marshal(linhas)
	previaJSON, _ := json.Marshal(plano.Previa)
	soma := sha256.Sum256(conteudo)
	nome := nomeArquivo(cabecalho.Filename)
	criada, err := s.q.CriarImportacao(ctx, db.CriarImportacaoParams{
		CriadaPor: u.ID, ArquivoNome: nome, ArquivoSha256: hex.EncodeToString(soma[:]),
		Linhas: linhasJSON, Previa: previaJSON,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "importacao_previa", "importacao", idTexto(criada.ID), map[string]any{"arquivo": nome, "totais": plano.Previa.Totais})

	responderJSON(w, http.StatusCreated, importacaoJSON{
		ID: criada.ID, Status: criada.Status, ArquivoNome: nome, CriadaPorNome: u.Nome,
		CriadaEm: criada.CriadaEm, Previa: plano.Previa,
	})
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

// aplicarImportacao grava a prévia no cadastro. Recalcula a prévia sobre o cadastro atual
// e recusa se ela mudou desde que o operador a revisou.
func (s *Servidor) aplicarImportacao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "importação não encontrada")
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

	if err := qtx.TravarAplicacaoDeImportacoes(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	imp, ok := s.travarImportacaoEmPrevia(w, r, qtx, id)
	if !ok {
		return
	}
	var armazenada importacao.Previa
	var linhas []importacao.Linha
	if err := json.Unmarshal(imp.Previa, &armazenada); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := json.Unmarshal(imp.Linhas, &linhas); err != nil {
		erroInterno(w, r, err)
		return
	}
	if armazenada.Totais.Erros > 0 {
		responderErro(w, http.StatusUnprocessableEntity, "a planilha tem erros: corrija e envie de novo")
		return
	}

	estado, err := carregarEstado(ctx, qtx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	plano := importacao.Planejar(linhas, nil, estado)
	if !importacao.MesmaPrevia(plano.Previa, armazenada) {
		responderErro(w, http.StatusConflict, "o cadastro mudou desde esta prévia: envie a planilha de novo para gerar uma prévia atualizada")
		return
	}
	if err := plano.Executar(ctx, qtx, id); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.FinalizarImportacao(ctx, db.FinalizarImportacaoParams{ID: id, Status: "aplicada"}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, &u.ID, "importacao_aplicada", "importacao", idTexto(id),
		map[string]any{"arquivo": imp.ArquivoNome, "totais": plano.Previa.Totais})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{"id": id, "status": "aplicada", "totais": plano.Previa.Totais})
}

func (s *Servidor) descartarImportacao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "importação não encontrada")
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

	imp, ok := s.travarImportacaoEmPrevia(w, r, qtx, id)
	if !ok {
		return
	}
	if err := qtx.FinalizarImportacao(ctx, db.FinalizarImportacaoParams{ID: id, Status: "descartada"}); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, &u.ID, "importacao_descartada", "importacao", idTexto(id),
		map[string]any{"arquivo": imp.ArquivoNome})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{"id": id, "status": "descartada"})
}

func (s *Servidor) travarImportacaoEmPrevia(w http.ResponseWriter, r *http.Request, qtx *db.Queries, id int64) (db.TravarImportacaoRow, bool) {
	imp, err := qtx.TravarImportacao(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "importação não encontrada")
		return imp, false
	}
	if err != nil {
		erroInterno(w, r, err)
		return imp, false
	}
	if imp.Status != "previa" {
		responderErro(w, http.StatusConflict, fmt.Sprintf("esta importação já foi %s", imp.Status))
		return imp, false
	}
	return imp, true
}

func carregarEstado(ctx context.Context, q *db.Queries) (importacao.Estado, error) {
	var e importacao.Estado
	clientes, err := q.ListarClientesParaImportacao(ctx)
	if err != nil {
		return e, err
	}
	cotas, err := q.ListarCotasParaImportacao(ctx)
	if err != nil {
		return e, err
	}
	for _, c := range clientes {
		e.Clientes = append(e.Clientes, importacao.ClienteAtual{
			ID: c.ID, Nome: c.Nome, NomeNormalizado: c.NomeNormalizado,
			Telefone: valorOuVazio(c.Telefone), Email: valorOuVazio(c.Email),
		})
	}
	for _, c := range cotas {
		dados := map[string]string{}
		if len(c.DadosPlanilha) > 0 {
			if err := json.Unmarshal(c.DadosPlanilha, &dados); err != nil {
				return e, fmt.Errorf("dados_planilha da cota %d: %w", c.ID, err)
			}
		}
		e.Cotas = append(e.Cotas, importacao.CotaAtual{
			ID: c.ID, ClienteID: c.ClienteID, ClienteNome: c.ClienteNome, ClienteNomeNormalizado: c.ClienteNomeNormalizado,
			Administradora: c.Administradora, Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao,
			TipoConsorcio: valorOuVazio(c.TipoConsorcio), Ativa: c.Ativa, DadosPlanilha: dados,
		})
	}
	return e, nil
}

func nomeArquivo(nome string) string {
	nome = strings.TrimSpace(filepath.Base(strings.ReplaceAll(nome, `\`, "/")))
	if nome == "" || nome == "." || nome == "/" {
		return "planilha.csv"
	}
	if len(nome) > 200 {
		nome = nome[:200]
	}
	return nome
}
