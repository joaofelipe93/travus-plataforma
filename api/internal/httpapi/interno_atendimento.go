package httpapi

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"
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
	}
	if err := lerJSON(w, r, &p, limiteMensagemBot); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	slug := strings.TrimSpace(strings.ToLower(p.Bot))
	telefone := reSoDigitos.ReplaceAllString(p.Telefone, "")
	texto := strings.TrimSpace(p.Texto)
	if slug == "" || telefone == "" || texto == "" {
		responderErro(w, http.StatusBadRequest, "informe bot, telefone e texto")
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

	res, err := s.responderAtendimento(ctx, bot, telefone, p.Contato, texto)
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
