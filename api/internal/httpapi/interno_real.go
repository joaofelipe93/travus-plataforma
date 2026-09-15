package httpapi

// Rotas internas do lance real e da reimpressão de comprovante (Etapa 3).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const limitePDF = 10 << 20

var (
	assinaturaPDF = []byte("%PDF-")
	soDigitos     = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// dataNewcon converte "15/09/2026" (formato das telas do Newcon) em data.
func dataNewcon(texto *string) *time.Time {
	if texto == nil {
		return nil
	}
	d, err := time.ParseInLocation("02/01/2006", strings.TrimSpace(*texto), time.Local)
	if err != nil {
		return nil
	}
	return &d
}

// marcarConfirmacaoIniciada grava confirmacao_iniciada. O worker só clica em Confirmar
// depois do 204. A chave de lance real, o cancelamento e a assembleia aprovada são
// conferidos de novo aqui, na mesma transação.
func (s *Servidor) marcarConfirmacaoIniciada(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p struct {
		AssembleiaData string `json:"assembleia_data"`
	}
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"assembleia_data": "DD/MM/AAAA"}`)
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
	switch {
	case c.Tipo != "real":
		responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("uma execução %s não confirma lance", c.Tipo))
		return
	case !s.cfg.LanceRealHabilitado:
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "lance real desligado neste ambiente (LANCE_REAL_HABILITADO)"})
		return
	case c.CancelamentoSolicitado:
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: "execução cancelada: não confirme", Cancelada: true})
		return
	case c.AssembleiaAprovada == nil || p.AssembleiaData != *c.AssembleiaAprovada:
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: fmt.Sprintf("assembleia %q diferente da aprovada na revisão (%s)", p.AssembleiaData, valorOuVazio(c.AssembleiaAprovada))})
		return
	}
	n, err := qtx.MarcarConfirmacaoIniciada(ctx, c.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: fmt.Sprintf("a cota %s está em %q: não confirme", tagDe(c), c.Status)})
		return
	}
	if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, "aviso", fmt.Sprintf("Cota %s: confirmação iniciada (o worker vai clicar em Confirmar no Newcon).", tagDe(c))); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, nil, "lance_confirmacao_iniciada", "execucao_cota", idTexto(c.ID),
		map[string]any{"cota": tagDe(c), "assembleia": p.AssembleiaData, "worker": worker, "execucao": c.ExecucaoID})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// enviarPdf guarda o comprovante (lance real ou reimpressão). PDFs não expiram.
func (s *Servidor) enviarPdf(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limitePDF)
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
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	c, ok := s.cotaDoWorker(w, r, qtx, worker)
	if !ok {
		return
	}
	if c.Tipo == "dry_run" {
		responderErro(w, http.StatusUnprocessableEntity, "dry-run não gera comprovante")
		return
	}
	soma := sha256.Sum256(conteudo)
	id, err := qtx.InserirArquivo(ctx, db.InserirArquivoParams{
		Tipo: "pdf", Nome: tagDe(c) + ".pdf", ContentType: "application/pdf",
		Tamanho: int32(len(conteudo)), Sha256: hex.EncodeToString(soma[:]), Conteudo: conteudo,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type pedidoConfirmacao struct {
	Status           string          `json:"status"`
	Protocolo        *string         `json:"protocolo"`
	TextoProtocolo   *string         `json:"texto_protocolo"`
	ParcelasEmAtraso bool            `json:"parcelas_em_atraso"`
	LanceExistente   bool            `json:"lance_existente"`
	PdfID            *string         `json:"pdf_id"`
	AssembleiaNumero *string         `json:"assembleia_numero"`
	Percentual       *string         `json:"percentual"`
	Erro             string          `json:"erro"`
	Detalhes         json.RawMessage `json:"detalhes"`
}

// concluirConfirmacao: resultado do clique em Confirmar. "confirmada" grava o lance.
func (s *Servidor) concluirConfirmacao(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p pedidoConfirmacao
	if err := lerJSON(w, r, &p, 64<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "resultado da confirmação inválido")
		return
	}
	switch {
	case p.Status != "confirmada" && p.Status != "erro_apos_confirmar":
		responderErro(w, http.StatusUnprocessableEntity, `status deve ser "confirmada" ou "erro_apos_confirmar"`)
		return
	case p.Protocolo != nil && !soDigitos.MatchString(*p.Protocolo):
		responderErro(w, http.StatusUnprocessableEntity, "protocolo inválido")
		return
	case p.Status == "confirmada" && p.Protocolo == nil:
		responderErro(w, http.StatusUnprocessableEntity, "lance confirmado precisa do protocolo")
		return
	case p.Status == "erro_apos_confirmar" && strings.TrimSpace(p.Erro) == "":
		responderErro(w, http.StatusUnprocessableEntity, "erro_apos_confirmar precisa da mensagem de erro")
		return
	case p.PdfID != nil && !formatoUUID.MatchString(*p.PdfID):
		responderErro(w, http.StatusUnprocessableEntity, "pdf_id inválido")
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
	if c.Tipo != "real" {
		responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("uma execução %s não confirma lance", c.Tipo))
		return
	}
	var erroTipo, erroTexto *string
	if p.Status == "erro_apos_confirmar" {
		inesperado := "inesperado"
		erroTipo, erroTexto = &inesperado, &p.Erro
	}
	n, err := qtx.ConcluirConfirmacao(ctx, db.ConcluirConfirmacaoParams{
		ID: c.ID, Status: p.Status, ErroTipo: erroTipo, Erro: erroTexto, Detalhes: objetoJSON(p.Detalhes), Protocolo: p.Protocolo,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: fmt.Sprintf("a cota %s não está em confirmação (está em %q)", tagDe(c), c.Status)})
		return
	}

	tag := tagDe(c)
	if p.Status == "confirmada" {
		driveStatus := "sem_pdf"
		if p.PdfID != nil {
			driveStatus = "pendente"
		}
		agora := s.agora()
		_, err := qtx.InserirLancePlataforma(ctx, db.InserirLancePlataformaParams{
			CotaID: c.CotaID, ExecucaoCotaID: &c.ID, Administradora: c.Administradora, Protocolo: *p.Protocolo,
			AssembleiaData: dataNewcon(c.AssembleiaAprovada), AssembleiaNumero: p.AssembleiaNumero, Modalidade: "2º Lance Fixo",
			Percentual: p.Percentual, TextoProtocolo: p.TextoProtocolo, ParcelasEmAtraso: p.ParcelasEmAtraso,
			LanceExistenteAutorizado: c.PermitirLanceExistente, PdfID: p.PdfID, DriveStatus: driveStatus, RegistradoEm: &agora,
		})
		var pgErr *pgconn.PgError
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, "aviso", fmt.Sprintf("Cota %s: o protocolo %s já estava registrado na plataforma.", tag, *p.Protocolo)); err != nil {
				erroInterno(w, r, err)
				return
			}
		case errors.As(err, &pgErr) && pgErr.Code == "23503":
			responderErro(w, http.StatusUnprocessableEntity, "pdf_id não corresponde a um arquivo enviado")
			return
		case err != nil:
			erroInterno(w, r, err)
			return
		}
		mensagem := fmt.Sprintf("Cota %s: lance registrado no Newcon, protocolo %s.", tag, *p.Protocolo)
		if p.ParcelasEmAtraso {
			mensagem += " Cota com parcelas em atraso."
		}
		if p.PdfID == nil {
			mensagem += " Sem comprovante: dá para reimprimir pelo Histórico."
		}
		if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, "ok", mensagem); err != nil {
			erroInterno(w, r, err)
			return
		}
		if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, nil, "lance_registrado", "execucao_cota", idTexto(c.ID), map[string]any{
			"cota": tag, "protocolo": *p.Protocolo, "parcelas_em_atraso": p.ParcelasEmAtraso,
			"aviso_ja_credenciado": p.LanceExistente, "com_pdf": p.PdfID != nil, "execucao": c.ExecucaoID,
		})); err != nil {
			erroInterno(w, r, err)
			return
		}
	} else {
		if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, "erro", fmt.Sprintf("Cota %s: %s", tag, p.Erro)); err != nil {
			erroInterno(w, r, err)
			return
		}
		if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, nil, "lance_erro_apos_confirmar", "execucao_cota", idTexto(c.ID), map[string]any{
			"cota": tag, "erro": p.Erro, "protocolo": p.Protocolo, "execucao": c.ExecucaoID,
		})); err != nil {
			erroInterno(w, r, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	if p.PdfID != nil {
		s.avisarDrive()
	}
	w.WriteHeader(http.StatusNoContent)
}

type pedidoReimpressao struct {
	Status         string `json:"status"`
	Protocolo      string `json:"protocolo"`
	PdfID          string `json:"pdf_id"`
	AssembleiaData string `json:"assembleia_data"`
	Credenciamento string `json:"credenciamento"`
	Modalidade     string `json:"modalidade"`
	Percentual     string `json:"percentual"`
}

// concluirReimpressao grava o comprovante obtido pelo Histórico (cria ou completa o lance).
func (s *Servidor) concluirReimpressao(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p pedidoReimpressao
	if err := lerJSON(w, r, &p, 16<<10); err != nil || p.Status != "reimpressa" || !soDigitos.MatchString(p.Protocolo) || !formatoUUID.MatchString(p.PdfID) {
		responderErro(w, http.StatusUnprocessableEntity, `envie status "reimpressa", protocolo e pdf_id`)
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
	if c.Tipo != "reimpressao" || c.Protocolo == nil || *c.Protocolo != p.Protocolo {
		responderErro(w, http.StatusUnprocessableEntity, "esta cota não é uma reimpressão desse protocolo")
		return
	}
	// O pedido de envio ao Drive foi gravado nos detalhes na criação da reimpressão.
	var pedido struct {
		EnviarDrive bool `json:"enviar_drive"`
	}
	_ = json.Unmarshal(c.Detalhes, &pedido)
	driveStatus := "nao_enviar"
	if pedido.EnviarDrive {
		driveStatus = "pendente"
	}
	detalhes, _ := json.Marshal(map[string]any{
		"assembleia_data": p.AssembleiaData, "credenciamento": p.Credenciamento, "modalidade": p.Modalidade, "percentual": p.Percentual,
		"enviar_drive": pedido.EnviarDrive,
	})
	n, err := qtx.ConcluirExecucaoCota(ctx, db.ConcluirExecucaoCotaParams{ID: c.ID, Status: "reimpressa", Detalhes: detalhes, Protocolo: &p.Protocolo})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		responderJSON(w, http.StatusConflict, respostaConflito{Erro: fmt.Sprintf("a cota %s não está em andamento (está em %q)", tagDe(c), c.Status)})
		return
	}
	var registrado *time.Time
	if t, err := time.ParseInLocation("02/01/2006 15:04:05", strings.TrimSpace(p.Credenciamento), time.Local); err == nil {
		registrado = &t
	}
	modalidade := strings.TrimSpace(p.Modalidade)
	if modalidade == "" {
		modalidade = "não informada"
	}
	lance, err := qtx.RegistrarComprovanteDoHistorico(ctx, db.RegistrarComprovanteDoHistoricoParams{
		CotaID: c.CotaID, ExecucaoCotaID: &c.ID, Administradora: c.Administradora, Protocolo: p.Protocolo,
		AssembleiaData: dataNewcon(&p.AssembleiaData), Modalidade: modalidade, Percentual: textoOuNil(strings.TrimSpace(p.Percentual)),
		PdfID: &p.PdfID, DriveStatus: driveStatus, RegistradoEm: registrado,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		responderErro(w, http.StatusUnprocessableEntity, "pdf_id não corresponde a um arquivo enviado")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	mensagem := fmt.Sprintf("Cota %s: comprovante do protocolo %s obtido pelo Histórico (nenhum lance foi registrado).", tagDe(c), p.Protocolo)
	if !pedido.EnviarDrive {
		mensagem += " O PDF ficou só na plataforma."
	}
	if err := s.evento(ctx, qtx, c.ExecucaoID, &c.ID, "ok", mensagem); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, nil, "comprovante_reimpresso", "lance", idTexto(lance.ID),
		map[string]any{"cota": tagDe(c), "protocolo": p.Protocolo, "origem_do_lance": lance.Origem, "enviar_drive": pedido.EnviarDrive})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	if pedido.EnviarDrive {
		s.avisarDrive()
	}
	w.WriteHeader(http.StatusNoContent)
}
