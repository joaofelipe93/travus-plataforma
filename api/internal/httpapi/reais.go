package httpapi

// Etapa 3 nas telas: revisão de dry-run, aprovação de lance real (só admin), reimpressão de
// comprovante e reenvio ao Drive.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const mensagemLanceRealDesligado = "lance real desligado neste ambiente (LANCE_REAL_HABILITADO=false)"

type lanceHistoricoJSON struct {
	Protocolo      string  `json:"protocolo"`
	Assembleia     string  `json:"assembleia"`
	Credenciamento *string `json:"credenciamento,omitempty"`
	Modalidade     *string `json:"modalidade,omitempty"`
	Percentual     *string `json:"percentual,omitempty"`
}

// Detalhes gravados pelo worker no dry-run (tela de credenciamento e Histórico).
type detalhesDryRun struct {
	AssembleiaData        *string              `json:"assembleia_data"`
	AssembleiaNumero      *string              `json:"assembleia_numero"`
	PercentualSegundoFixo *string              `json:"percentual_segundo_fixo"`
	HistoricoLido         bool                 `json:"historico_lido"`
	LancesNestaAssembleia int                  `json:"lances_nesta_assembleia"`
	Lances                []lanceHistoricoJSON `json:"lances"`
}

type cotaRevisaoJSON struct {
	ExecucaoCotaID        int64                `json:"execucao_cota_id"`
	CotaID                int64                `json:"cota_id"`
	Grupo                 string               `json:"grupo"`
	Cota                  string               `json:"cota"`
	Versao                string               `json:"versao"`
	ClienteNome           string               `json:"cliente_nome"`
	Ativa                 bool                 `json:"ativa"`
	AssembleiaData        *string              `json:"assembleia_data"`
	AssembleiaNumero      *string              `json:"assembleia_numero"`
	PercentualSegundoFixo *string              `json:"percentual_segundo_fixo"`
	HistoricoLido         bool                 `json:"historico_lido"`
	LancesNestaAssembleia int                  `json:"lances_nesta_assembleia"`
	Lances                []lanceHistoricoJSON `json:"lances"`
	LancesPlataforma      []string             `json:"lances_plataforma"`
	ExigeAutorizacao      bool                 `json:"exige_autorizacao"`
	ScreenshotID          *string              `json:"screenshot_id"`
	Bloqueio              string               `json:"bloqueio"`

	modalidade string
	detalhes   []byte
}

type revisaoJSON struct {
	DryRun struct {
		ID             int64      `json:"id"`
		Status         string     `json:"status"`
		FinalizadaEm   *time.Time `json:"finalizada_em"`
		ValidoAte      *time.Time `json:"valido_ate"`
		Expirado       bool       `json:"expirado"`
		ExecucaoRealID *int64     `json:"execucao_real_id"`
	} `json:"dry_run"`
	LanceRealHabilitado bool              `json:"lance_real_habilitado"`
	PodeAprovar         bool              `json:"pode_aprovar"`
	Bloqueio            string            `json:"bloqueio"`
	Cotas               []cotaRevisaoJSON `json:"cotas"`
}

var errNaoEncontrado = errors.New("não encontrado")

type erroValidacao string

func (e erroValidacao) Error() string { return string(e) }

// textoDuracao escreve prazos para a tela: "2 h", "90 min".
func textoDuracao(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d h", int(d/time.Hour))
	}
	return fmt.Sprintf("%d min", int(d/time.Minute))
}

// montarRevisao reúne o que o admin precisa ver antes de aprovar e o que a aprovação confere.
func (s *Servidor) montarRevisao(ctx context.Context, q *db.Queries, id int64) (revisaoJSON, error) {
	var rv revisaoJSON
	e, err := q.BuscarExecucao(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return rv, errNaoEncontrado
	}
	if err != nil {
		return rv, err
	}
	if e.Tipo != "dry_run" {
		return rv, erroValidacao("só um dry-run pode ser revisado para lance real")
	}
	rv.DryRun.ID, rv.DryRun.Status, rv.DryRun.FinalizadaEm = e.ID, e.Status, e.FinalizadaEm
	rv.LanceRealHabilitado = s.cfg.LanceRealHabilitado
	if e.FinalizadaEm != nil {
		ate := e.FinalizadaEm.Add(s.cfg.ValidadeDryRun)
		rv.DryRun.ValidoAte, rv.DryRun.Expirado = &ate, s.agora().After(ate)
	}
	real, err := q.ExecucaoRealDoDryRun(ctx, &id)
	if err == nil {
		rv.DryRun.ExecucaoRealID = &real.ID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return rv, err
	}

	switch {
	case e.Status != "concluida" && e.Status != "concluida_com_erros":
		rv.Bloqueio = "o dry-run não terminou com sucesso (ainda em andamento, cancelado ou falhou)"
	case rv.DryRun.ExecucaoRealID != nil:
		rv.Bloqueio = fmt.Sprintf("este dry-run já foi aprovado na execução nº %d", *rv.DryRun.ExecucaoRealID)
	case rv.DryRun.Expirado:
		rv.Bloqueio = fmt.Sprintf("o dry-run passou do prazo de %s para aprovação: faça um novo dry-run", textoDuracao(s.cfg.ValidadeDryRun))
	case !s.cfg.LanceRealHabilitado:
		rv.Bloqueio = mensagemLanceRealDesligado
	}

	rows, err := q.CotasVerificadasDoDryRun(ctx, id)
	if err != nil {
		return rv, err
	}
	rv.Cotas = []cotaRevisaoJSON{}
	algumaLiberada := false
	for _, c := range rows {
		var d detalhesDryRun
		_ = json.Unmarshal(c.Detalhes, &d)
		cr := cotaRevisaoJSON{
			ExecucaoCotaID: c.ID, CotaID: c.CotaID, Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao, ClienteNome: c.ClienteNome,
			Ativa: c.Ativa, AssembleiaData: d.AssembleiaData, AssembleiaNumero: d.AssembleiaNumero,
			PercentualSegundoFixo: d.PercentualSegundoFixo, HistoricoLido: d.HistoricoLido, LancesNestaAssembleia: d.LancesNestaAssembleia,
			Lances: d.Lances, LancesPlataforma: []string{}, ScreenshotID: c.ScreenshotID, modalidade: c.Modalidade, detalhes: c.Detalhes,
		}
		if cr.Lances == nil {
			cr.Lances = []lanceHistoricoJSON{}
		}
		if data := dataNewcon(d.AssembleiaData); data != nil {
			doHistorico := map[string]bool{}
			for _, l := range cr.Lances {
				doHistorico[l.Protocolo] = true
			}
			lances, err := q.LancesDaCotaNaAssembleia(ctx, db.LancesDaCotaNaAssembleiaParams{CotaID: c.CotaID, AssembleiaData: data})
			if err != nil {
				return rv, err
			}
			for _, l := range lances {
				if !doHistorico[l.Protocolo] {
					cr.LancesPlataforma = append(cr.LancesPlataforma, l.Protocolo)
				}
			}
		}
		switch {
		case !c.Ativa:
			cr.Bloqueio = "cota desativada depois do dry-run"
		case d.AssembleiaData == nil:
			cr.Bloqueio = "a assembleia não foi lida no dry-run"
		}
		cr.ExigeAutorizacao = d.LancesNestaAssembleia > 0 || len(cr.LancesPlataforma) > 0
		algumaLiberada = algumaLiberada || cr.Bloqueio == ""
		rv.Cotas = append(rv.Cotas, cr)
	}
	rv.PodeAprovar = rv.Bloqueio == "" && algumaLiberada
	return rv, nil
}

func (s *Servidor) revisaoDryRun(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	rv, err := s.montarRevisao(r.Context(), s.q, id)
	if s.responderErroRevisao(w, r, err) {
		return
	}
	responderJSON(w, http.StatusOK, rv)
}

func (s *Servidor) responderErroRevisao(w http.ResponseWriter, r *http.Request, err error) bool {
	var ev erroValidacao
	switch {
	case err == nil:
		return false
	case errors.Is(err, errNaoEncontrado):
		responderErro(w, http.StatusNotFound, "execução não encontrada")
	case errors.As(err, &ev):
		responderErro(w, http.StatusUnprocessableEntity, ev.Error())
	default:
		erroInterno(w, r, err)
	}
	return true
}

type pedidoAprovacao struct {
	DryRunID int64 `json:"dry_run_id"`
	Cotas    []struct {
		ExecucaoCotaID         int64 `json:"execucao_cota_id"`
		RegistrarMesmoComLance bool  `json:"registrar_mesmo_com_lance"`
	} `json:"cotas"`
	QuantidadeConfirmada int `json:"quantidade_confirmada"`
}

// aprovarExecucaoReal cria a execução real a partir de um dry-run revisado. Só admin.
func (s *Servidor) aprovarExecucaoReal(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoAprovacao
	if err := lerJSON(w, r, &p, 64<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "pedido de aprovação inválido")
		return
	}
	if !s.cfg.LanceRealHabilitado {
		responderErro(w, http.StatusForbidden, mensagemLanceRealDesligado)
		return
	}
	if len(p.Cotas) == 0 {
		responderErro(w, http.StatusUnprocessableEntity, "escolha pelo menos uma cota")
		return
	}
	if p.QuantidadeConfirmada != len(p.Cotas) {
		responderErro(w, http.StatusUnprocessableEntity, "a quantidade digitada não confere com as cotas escolhidas")
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

	// Trava o dry-run: duas aprovações ao mesmo tempo não criam duas execuções reais.
	if _, err := qtx.TravarExecucao(ctx, p.DryRunID); errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "dry-run não encontrado")
		return
	} else if err != nil {
		erroInterno(w, r, err)
		return
	}
	rv, err := s.montarRevisao(ctx, qtx, p.DryRunID)
	if s.responderErroRevisao(w, r, err) {
		return
	}
	if rv.Bloqueio != "" {
		responderErro(w, http.StatusConflict, rv.Bloqueio)
		return
	}
	porID := map[int64]cotaRevisaoJSON{}
	for _, c := range rv.Cotas {
		porID[c.ExecucaoCotaID] = c
	}

	real, err := qtx.CriarExecucaoReal(ctx, db.CriarExecucaoRealParams{UsuarioID: u.ID, DryRunOrigemID: &p.DryRunID})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	vistas := map[int64]bool{}
	var tags, autorizadas []string
	for i, escolha := range p.Cotas {
		c, ok := porID[escolha.ExecucaoCotaID]
		tag := c.Grupo + "-" + c.Cota + "-" + c.Versao
		switch {
		case !ok || vistas[escolha.ExecucaoCotaID]:
			responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("a cota %d não está pronta neste dry-run (ou foi repetida)", escolha.ExecucaoCotaID))
			return
		case c.Bloqueio != "":
			responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("cota %s: %s", tag, c.Bloqueio))
			return
		case c.ExigeAutorizacao && !escolha.RegistrarMesmoComLance:
			responderErro(w, http.StatusUnprocessableEntity, fmt.Sprintf("a cota %s já tem lance nesta assembleia: marque \"registrar mesmo assim\" ou tire da lista", tag))
			return
		}
		vistas[escolha.ExecucaoCotaID] = true
		permitir := c.ExigeAutorizacao && escolha.RegistrarMesmoComLance
		if permitir {
			autorizadas = append(autorizadas, tag)
		}
		if err := qtx.InserirCotaReal(ctx, db.InserirCotaRealParams{
			ExecucaoID: real.ID, CotaID: c.CotaID, Ordem: int32(i + 1), Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao,
			ClienteNome: c.ClienteNome, Modalidade: c.modalidade, AssembleiaAprovada: c.AssembleiaData,
			PermitirLanceExistente: permitir, Detalhes: c.detalhes,
		}); err != nil {
			erroInterno(w, r, err)
			return
		}
		tags = append(tags, tag)
	}

	resumo := fmt.Sprintf("Lance real aprovado por %s a partir do dry-run nº %d: %d cota(s) (%s).", u.Nome, p.DryRunID, len(tags), strings.Join(tags, ", "))
	if err := s.evento(ctx, qtx, real.ID, nil, "aviso", resumo); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := s.evento(ctx, qtx, p.DryRunID, nil, "info", fmt.Sprintf("Aprovado para lance real na execução nº %d por %s.", real.ID, u.Nome)); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, &u.ID, "execucao_real_aprovada", "execucao", idTexto(real.ID), map[string]any{
		"dry_run": p.DryRunID, "cotas": tags, "autorizadas_com_lance_existente": autorizadas,
	})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]any{"id": real.ID, "status": real.Status})
}

// criarReimpressao busca no Histórico do Newcon o comprovante de um protocolo. Não registra lance.
func (s *Servidor) criarReimpressao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p struct {
		CotaID    int64  `json:"cota_id"`
		Protocolo string `json:"protocolo"`
		// Sem pedido, o PDF fica só na plataforma (drive_status nao_enviar).
		EnviarDrive bool `json:"enviar_drive"`
	}
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"cota_id": ..., "protocolo": "...", "enviar_drive": false}`)
		return
	}
	p.Protocolo = strings.TrimSpace(p.Protocolo)
	if !soDigitos.MatchString(p.Protocolo) {
		responderErro(w, http.StatusUnprocessableEntity, "protocolo inválido: só números")
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

	exec, err := qtx.CriarExecucaoReimpressao(ctx, u.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	pedido, _ := json.Marshal(map[string]bool{"enviar_drive": p.EnviarDrive})
	n, err := qtx.InserirCotaReimpressao(ctx, db.InserirCotaReimpressaoParams{ExecucaoID: exec.ID, Protocolo: p.Protocolo, CotaID: p.CotaID, Detalhes: pedido})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		responderErro(w, http.StatusNotFound, "cota não encontrada")
		return
	}
	destino := "o PDF fica só na plataforma"
	if p.EnviarDrive {
		destino = "o PDF vai para o Google Drive"
	}
	if err := s.evento(ctx, qtx, exec.ID, nil, "info", fmt.Sprintf("Reimpressão do comprovante do protocolo %s pedida por %s: não registra lance; %s.", p.Protocolo, u.Nome, destino)); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(r, &u.ID, "reimpressao_pedida", "execucao", idTexto(exec.ID),
		map[string]any{"cota_id": p.CotaID, "protocolo": p.Protocolo, "enviar_drive": p.EnviarDrive})); err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusCreated, map[string]any{"id": exec.ID, "status": exec.Status})
}

func (s *Servidor) reenviarAoDrive(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "lance não encontrado")
		return
	}
	ctx := r.Context()
	n, err := s.q.ReenviarAoDrive(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if n == 0 {
		if _, err := s.q.BuscarLance(ctx, id); errors.Is(err, pgx.ErrNoRows) {
			responderErro(w, http.StatusNotFound, "lance não encontrado")
			return
		}
		responderErro(w, http.StatusConflict, "só dá para enviar ao Drive comprovantes com erro no envio ou que ficaram sem envio")
		return
	}
	s.auditar(ctx, r, &u.ID, "drive_reenvio_pedido", "lance", idTexto(id), nil)
	s.avisarDrive()
	responderJSON(w, http.StatusOK, map[string]any{"id": id, "drive_status": "pendente"})
}

func (s *Servidor) situacaoGoogleDrive(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	g := s.cfg.Google
	resp := map[string]any{
		"lance_real_habilitado": s.cfg.LanceRealHabilitado,
		"cofre_configurado":     s.cfg.Cofre != nil,
		"client_configurado":    g.ClientID != "" && g.ClientSecret != "",
		"pasta_configurada":     g.PastaDrive != "",
		"token_importado":       false,
		"token_atualizado_em":   nil,
	}
	integ, err := s.q.BuscarIntegracao(r.Context(), IntegracaoGoogleDrive)
	if err == nil {
		resp["token_importado"], resp["token_atualizado_em"] = true, integ.AtualizadoEm
	} else if !errors.Is(err, pgx.ErrNoRows) {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, resp)
}
