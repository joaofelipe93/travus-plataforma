package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Rotas internas do serviço de Atendimento (porta 8081, token de serviço): é por aqui que o
// worker de WhatsApp entrega o que recebeu e descobre o que fazer.
//
// O worker não decide nada — ele não sabe o que é etapa, teto ou ensaio. Manda "o telefone X
// escreveu isto no bot Y" e recebe de volta o que aconteceu; a resposta, quando há uma, fica na
// fila para ele enviar. Dois motivos para ser assim: a chave da Anthropic não sai da API, e a
// peça que mais reinicia (o socket do WhatsApp) fica sem regra de negócio dentro.
func (s *Servidor) rotasInternasAtendimento(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/atendimento/mensagens", s.receberMensagemDeBot)
	mux.HandleFunc("GET /internal/atendimento/bots", s.botsParaOWorker)
	// Etapa D: a fila de envio.
	mux.HandleFunc("POST /internal/atendimento/fila/proxima", s.proximasMensagensDeBot)
	mux.HandleFunc("POST /internal/atendimento/mensagens/{id}/enviada", s.mensagemDeBotEnviada)
	mux.HandleFunc("POST /internal/atendimento/mensagens/{id}/falhou", s.mensagemDeBotFalhou)
}

// Só dígitos: o jid do WhatsApp (`5511...@s.whatsapp.net`) é assunto do worker, e o que entra no
// banco é o número. O CHECK da tabela exige o mesmo.
var reSoDigitos = regexp.MustCompile(`\D`)

const limiteMensagemBot = 8 << 10

// POST /internal/atendimento/mensagens — uma mensagem que chegou no WhatsApp de um bot.
func (s *Servidor) receberMensagemDeBot(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p struct {
		Bot      string `json:"bot"`      // o slug
		Telefone string `json:"telefone"` // só dígitos; o worker manda o número do jid
		Contato  string `json:"contato"`  // nome que aparece no WhatsApp, quando vem
		Texto    string `json:"texto"`
		// Mensagem que SAIU pelo número do bot (o `fromMe` do WhatsApp): ou é o eco da resposta
		// que a plataforma mandou, ou é um consultor escrevendo pelo celular (issue #128). Quem
		// decide qual é a API, pelo `wa_id` — o worker não sabe o que a plataforma enviou.
		DoNumeroDoBot bool   `json:"do_numero_do_bot"`
		WaID          string `json:"wa_id"`
		// O identificador LID da conversa, quando o WhatsApp endereça por ele em vez de por
		// telefone: é assim que se reconhece a conversa de uma mensagem que saiu pelo bot.
		Lid string `json:"lid"`
	}
	if err := lerJSON(w, r, &p, limiteMensagemBot); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	slug := strings.TrimSpace(strings.ToLower(p.Bot))
	telefone := reSoDigitos.ReplaceAllString(p.Telefone, "")
	lid := reSoDigitos.ReplaceAllString(p.Lid, "")
	texto := strings.TrimSpace(p.Texto)
	if slug == "" || texto == "" {
		responderErro(w, http.StatusBadRequest, "informe bot e texto")
		return
	}
	// Mensagem recebida sem telefone não vira conversa: a plataforma identifica a pessoa pelo
	// número. A que saiu pelo bot pode vir só com o LID — a conversa é achada por ele.
	if telefone == "" && !(p.DoNumeroDoBot && lid != "") {
		responderErro(w, http.StatusBadRequest, "informe o telefone")
		return
	}
	// Mensagem muito grande é cortada em vez de recusada: o que chegou já chegou, e devolver
	// erro faria o worker tentar de novo para sempre.
	if r := []rune(texto); len(r) > 4000 {
		texto = string(r[:4000])
	}

	ctx := r.Context()
	bot, achou, err := s.botDoSlug(ctx, slug)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if !achou {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return
	}

	if p.DoNumeroDoBot {
		s.mensagemDoNumeroDoBot(w, r, worker, bot, telefone, p.Contato, texto, strings.TrimSpace(p.WaID), lid)
		return
	}

	res, err := s.responderAtendimento(ctx, bot, telefone, p.Contato, texto, lid)
	if err != nil {
		// A mensagem recebida já está gravada; o erro é na resposta. O worker não repete (ele
		// repetiria a conversa inteira) — quem olha isso é o log e, depois, o vigia.
		slog.Error("atendimento: falha ao responder", "worker", worker, "bot", slug,
			"conversa_id", res.ConversaID, "erro", err)
		responderErro(w, http.StatusBadGateway, "não foi possível gerar a resposta agora")
		return
	}
	slog.Info("atendimento: mensagem tratada", "worker", worker, "bot", slug,
		"conversa_id", res.ConversaID, "status", res.Status, "motivo", res.Motivo,
		"encerrada", res.Encerrada, "tokens", res.TokensGasto)
	responderJSON(w, http.StatusOK, res)
}

// mensagemDoNumeroDoBot trata o que saiu pelo WhatsApp do bot. Duas coisas chegam por aqui:
//
//   - o **eco da nossa própria resposta**: o WhatsApp devolve no evento tudo o que o número
//     mandou, inclusive o que a plataforma acabou de enviar. Reconhecido pelo `wa_id` que o
//     worker devolveu no envio, e ignorado — senão todo bot se daria por assumido na primeira
//     resposta que mandasse;
//   - um **consultor escrevendo pelo celular**: aí a conversa passa a ser dele (issue #128).
//
// Sem `wa_id` não há como distinguir, e o seguro é ignorar: um bot que emudece sozinho é pior que
// um bot que não detectou a intervenção (para esse caso existe o botão na tela).
func (s *Servidor) mensagemDoNumeroDoBot(w http.ResponseWriter, r *http.Request, worker string,
	bot db.AtendimentoBot, telefone, contato, texto, waID, lid string) {
	ctx := r.Context()
	if waID == "" {
		slog.Warn("atendimento: mensagem do número do bot sem wa_id: ignorada",
			"worker", worker, "bot", bot.Slug)
		responderJSON(w, http.StatusOK, resultadoAtendimento{Status: "sem_resposta", Motivo: "sem wa_id"})
		return
	}
	switch _, err := s.q.MensagemNossaPeloWaID(ctx, &waID); {
	case err == nil:
		// É o eco da resposta que a plataforma mandou.
		responderJSON(w, http.StatusOK, resultadoAtendimento{Status: "sem_resposta", Motivo: "eco da própria resposta"})
		return
	case !errors.Is(err, pgx.ErrNoRows):
		erroInterno(w, r, err)
		return
	}

	res, err := s.registrarMensagemDePessoa(ctx, bot, telefone, contato, texto, waID, lid)
	if err != nil {
		slog.Error("atendimento: falha ao registrar mensagem de pessoa", "worker", worker,
			"bot", bot.Slug, "conversa_id", res.ConversaID, "erro", err)
		responderErro(w, http.StatusBadGateway, "não foi possível registrar a mensagem agora")
		return
	}
	slog.Info("atendimento: mensagem escrita por uma pessoa pelo número do bot",
		"worker", worker, "bot", bot.Slug, "conversa_id", res.ConversaID, "motivo", res.Motivo)
	responderJSON(w, http.StatusOK, res)
}

// GET /internal/atendimento/bots — os bots que o worker deve conectar, com o que ele precisa
// saber: o slug (pasta da sessão) e o ritmo. Instruções e modelo ficam na API de propósito.
func (s *Servidor) botsParaOWorker(w http.ResponseWriter, r *http.Request) {
	if _, ok := nomeWorker(w, r); !ok {
		return
	}
	linhas, err := s.q.ListarBots(r.Context())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	type botDoWorker struct {
		Slug                  string `json:"slug"`
		Nome                  string `json:"nome"`
		Ativo                 bool   `json:"ativo"`
		PausaRespostaSegundos int32  `json:"pausa_resposta_segundos"`
	}
	bots := make([]botDoWorker, 0, len(linhas))
	for _, l := range linhas {
		if !l.Ativo {
			continue
		}
		bots = append(bots, botDoWorker{
			Slug: l.Slug, Nome: l.Nome, Ativo: l.Ativo, PausaRespostaSegundos: l.PausaRespostaSegundos,
		})
	}
	responderJSON(w, http.StatusOK, map[string]any{"bots": bots})
}

// ---------------------------------------------------------------------------
// Etapa D: a fila de envio. O worker de WhatsApp pede trabalho e devolve o resultado.
// ---------------------------------------------------------------------------

// Tentativas antes de desistir de uma resposta, e a espera entre elas. Resposta de atendimento
// envelhece rápido — quem escreveu está esperando —, então são poucas tentativas e perto uma da
// outra: 30s, 1min, 2min, 4min. Depois disso, o problema não é passageiro.
const (
	maxTentativasAtendimento = 5
	esperaBaseAtendimento    = 30 * time.Second
)

// esperaDaTentativa dobra a espera a cada tentativa, com teto.
func esperaDaTentativa(tentativas int32) time.Duration {
	if tentativas < 1 {
		tentativas = 1
	}
	espera := esperaBaseAtendimento << (tentativas - 1)
	if espera > 10*time.Minute {
		espera = 10 * time.Minute
	}
	return espera
}

// POST /internal/atendimento/fila/proxima — o worker diz quais bots ele tem conectados e leva o
// que há para enviar neles. Bot que ele não conseguiu conectar não entra: a mensagem fica na
// fila em vez de ser marcada como entregue por um número que não existe.
func (s *Servidor) proximasMensagensDeBot(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	var p struct {
		Bots   []string `json:"bots"`   // slugs com sessão aberta
		Limite int      `json:"limite"` // quantas levar neste ciclo
	}
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	slugs := make([]string, 0, len(p.Bots))
	for _, b := range p.Bots {
		if b = strings.TrimSpace(strings.ToLower(b)); b != "" {
			slugs = append(slugs, b)
		}
	}
	if len(slugs) == 0 {
		// Nenhuma sessão aberta: nada sai, e isso não é erro.
		responderJSON(w, http.StatusOK, map[string]any{"mensagens": []any{}})
		return
	}
	limite := p.Limite
	if limite < 1 || limite > 50 {
		limite = 10
	}

	linhas, err := s.q.ReservarMensagensDeAtendimento(r.Context(), db.ReservarMensagensDeAtendimentoParams{
		Slugs: slugs, Limite: int32(limite),
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	type mensagemDoWorker struct {
		ID         int64  `json:"id"`
		ConversaID int64  `json:"conversa_id"`
		Bot        string `json:"bot"`
		Telefone   string `json:"telefone"`
		Texto      string `json:"texto"`
		Tentativa  int32  `json:"tentativa"`
	}
	msgs := make([]mensagemDoWorker, 0, len(linhas))
	for _, l := range linhas {
		msgs = append(msgs, mensagemDoWorker{
			ID: l.ID, ConversaID: l.ConversaID, Bot: l.BotSlug,
			Telefone: l.Telefone, Texto: l.Texto, Tentativa: l.Tentativas,
		})
	}
	if len(msgs) > 0 {
		slog.Info("atendimento: fila entregue ao worker", "worker", worker, "bots", slugs, "mensagens", len(msgs))
	}
	responderJSON(w, http.StatusOK, map[string]any{"mensagens": msgs})
}

// POST /internal/atendimento/mensagens/{id}/enviada
func (s *Servidor) mensagemDeBotEnviada(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	// O id da mensagem no WhatsApp vem no corpo: é o que depois distingue o eco da nossa própria
	// resposta de um consultor escrevendo pelo celular (issue #128). Corpo vazio continua
	// valendo, para um worker antigo não deixar de registrar o envio por causa disto.
	var p struct {
		WaID string `json:"wa_id"`
	}
	_ = lerJSON(w, r, &p, 1<<12)
	linha, err := s.q.MarcarMensagemDeAtendimentoEnviada(r.Context(), db.MarcarMensagemDeAtendimentoEnviadaParams{
		ID: id, WaID: textoOuNil(strings.TrimSpace(p.WaID)),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Já não estava pendente: outro ciclo registrou, ou alguém cancelou. Não é erro — e o
		// worker não pode reagir reenviando.
		responderJSON(w, http.StatusOK, map[string]any{"registrada": false})
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	slog.Info("atendimento: resposta entregue", "worker", worker, "mensagem_id", linha.ID,
		"conversa_id", linha.ConversaID)
	responderJSON(w, http.StatusOK, map[string]any{"registrada": true})
}

// POST /internal/atendimento/mensagens/{id}/falhou — o worker não conseguiu entregar.
//
// Quem decide entre tentar de novo e desistir é a API: o worker só informa o erro e se ele é
// definitivo (número sem WhatsApp não melhora tentando de novo).
func (s *Servidor) mensagemDeBotFalhou(w http.ResponseWriter, r *http.Request) {
	worker, ok := nomeWorker(w, r)
	if !ok {
		return
	}
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	var p struct {
		Erro       string `json:"erro"`
		Definitivo bool   `json:"definitivo"`
		Tentativa  int32  `json:"tentativa"`
	}
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	motivo := strings.TrimSpace(p.Erro)
	if motivo == "" {
		motivo = "falha ao enviar"
	}
	if len(motivo) > 500 {
		motivo = motivo[:500]
	}

	ctx := r.Context()
	desistir := p.Definitivo || p.Tentativa >= maxTentativasAtendimento
	if desistir {
		linha, err := s.q.DesistirDaMensagemDeAtendimento(ctx, db.DesistirDaMensagemDeAtendimentoParams{
			ID: id, Motivo: &motivo, Erro: &motivo,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			responderJSON(w, http.StatusOK, map[string]any{"status": "ja_resolvida"})
			return
		}
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		slog.Warn("atendimento: resposta não entregue", "worker", worker, "mensagem_id", linha.ID,
			"tentativas", linha.Tentativas, "definitivo", p.Definitivo, "erro", motivo)
		responderJSON(w, http.StatusOK, map[string]any{"status": "falhou", "tentativas": linha.Tentativas})
		return
	}

	espera := esperaDaTentativa(p.Tentativa)
	linha, err := s.q.ReenfileirarMensagemDeAtendimento(ctx, db.ReenfileirarMensagemDeAtendimentoParams{
		ID: id, Erro: &motivo, EsperaSegundos: espera.Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderJSON(w, http.StatusOK, map[string]any{"status": "ja_resolvida"})
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	slog.Info("atendimento: resposta volta à fila", "worker", worker, "mensagem_id", linha.ID,
		"tentativas", linha.Tentativas, "proxima_em", linha.ProximaTentativaEm, "erro", motivo)
	responderJSON(w, http.StatusOK, map[string]any{
		"status": "pendente", "tentativas": linha.Tentativas, "proxima_tentativa_em": linha.ProximaTentativaEm,
	})
}
