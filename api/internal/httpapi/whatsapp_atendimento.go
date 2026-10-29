package httpapi

// Etapa D da issue #100: a tela de Conexões do Atendimento, só admin.
//
// Quem fala com o worker é a API: o `ATENDIMENTO_ADMIN_TOKEN` nunca chega ao navegador, porque
// **quem tem o QR pareia o próprio aparelho na conta** daquele bot. O mesmo cuidado das telas de
// WhatsApp dos boletos e do notificador.
//
// Worker fora do ar não é erro da tela: responde 200 com `disponivel: false` e o motivo, para a
// tela dizer "o serviço não está rodando" em vez de um 502 cru.

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/atendimentowa"
)

// Telefone do teste: só dígitos, com DDI. Quem digita é um admin na tela, e a mensagem sai para
// um telefone de verdade — então a conferência é aqui, antes de chegar ao worker.
var reTelefoneTeste = regexp.MustCompile(`^[0-9]{12,15}$`)

// GET /atendimento/whatsapp — as sessões do worker, com a fila de cada bot.
func (s *Servidor) situacaoWhatsappAtendimento(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	ctx := r.Context()

	// A fila sai do banco: ela existe mesmo com o worker desligado, e é justamente o que
	// explica "o bot respondeu mas ninguém recebeu".
	filaPorBot := map[int64]map[string]int64{}
	linhas, err := s.q.FilaDeAtendimentoPorBot(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	for _, l := range linhas {
		if filaPorBot[l.BotID] == nil {
			filaPorBot[l.BotID] = map[string]int64{}
		}
		filaPorBot[l.BotID][l.Status] = l.Total
	}

	bots, err := s.q.ListarBots(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}

	resposta := map[string]any{
		"disponivel":        false,
		"modelo_disponivel": s.cfg.ModeloAtendimento != nil,
	}

	status, erroWorker := s.cfg.AtendimentoWhatsapp.Status(ctx)
	if erroWorker == nil {
		resposta["disponivel"] = true
	} else {
		resposta["motivo"] = erroWorker.Error()
	}

	type botNaTela struct {
		ID    int64  `json:"id"`
		Nome  string `json:"nome"`
		Slug  string `json:"slug"`
		Ativo bool   `json:"ativo"`
		// Estado no vocabulário das outras telas de WhatsApp da plataforma, para a tela usar as
		// mesmas cores e rótulos (components/conexao-whatsapp.tsx).
		Estado string `json:"estado"`
		// O worker está no ar e não tem sessão para este bot: normal com o bot desligado, ou
		// quando ele passou do teto de sessões. A tela explica a diferença.
		SemSessao  bool             `json:"sem_sessao"`
		Conectado  bool             `json:"conectado"`
		Numero     *string          `json:"numero"`
		QR         *string          `json:"qr"`
		AbertaHaMs *int64           `json:"aberta_ha_ms"`
		UltimoErro *string          `json:"ultimo_erro"`
		Fila       map[string]int64 `json:"fila"`
	}
	lista := make([]botNaTela, 0, len(bots))
	for _, b := range bots {
		item := botNaTela{
			ID: b.ID, Nome: b.Nome, Slug: b.Slug, Ativo: b.Ativo,
			Estado: "indisponivel", Fila: filaPorBot[b.ID],
		}
		if item.Fila == nil {
			item.Fila = map[string]int64{}
		}
		if erroWorker == nil {
			if sessao := status.Da(b.Slug); sessao != nil {
				item.Estado = estadoNaTela(sessao.Estado)
				item.Conectado = sessao.Conectado
				item.Numero = sessao.Numero
				item.QR = sessao.QR
				item.AbertaHaMs = sessao.AbertaHaMs
				item.UltimoErro = sessao.UltimoErro
			} else {
				// O worker está no ar e não tem sessão para este bot: normal quando o bot está
				// desligado (ele só conecta bot ligado) ou quando passou do teto de sessões.
				item.Estado = "desconectado"
				item.SemSessao = true
			}
		}
		lista = append(lista, item)
	}
	resposta["bots"] = lista
	responderJSON(w, http.StatusOK, resposta)
}

// botDoCaminho busca o bot do id na rota. Devolve false já tendo respondido.
func (s *Servidor) botDoCaminho(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return "", false
	}
	bot, err := s.q.BuscarBot(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return "", false
	}
	if err != nil {
		erroInterno(w, r, err)
		return "", false
	}
	return bot.Slug, true
}

// POST /atendimento/bots/{id}/whatsapp/teste — mensagem de teste pelo número do bot.
//
// **Sai para um telefone de verdade, mesmo em modo ensaio**: é com ela que se confere se o
// número pareado é o certo. Fica auditada com o telefone mascarado.
func (s *Servidor) testarWhatsappAtendimento(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	slug, ok := s.botDoCaminho(w, r)
	if !ok {
		return
	}
	var p struct {
		Telefone string `json:"telefone"`
	}
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	telefone := reSoDigitos.ReplaceAllString(p.Telefone, "")
	if !reTelefoneTeste.MatchString(telefone) {
		responderErro(w, http.StatusBadRequest, "informe o telefone com DDI e DDD, só dígitos (ex.: 5511999999999)")
		return
	}

	if err := s.cfg.AtendimentoWhatsapp.EnviarTeste(r.Context(), slug, telefone); err != nil {
		s.responderErroDoWorkerDeAtendimento(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "atendimento_whatsapp_teste", "bot", slug, map[string]any{
		"telefone": mascararTelefone(telefone),
	})
	responderJSON(w, http.StatusAccepted, map[string]any{"enviada": true})
}

// POST /atendimento/bots/{id}/whatsapp/desconectar — desvincula o aparelho e gera QR novo.
func (s *Servidor) desconectarWhatsappAtendimento(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	slug, ok := s.botDoCaminho(w, r)
	if !ok {
		return
	}
	var p struct {
		Confirmar bool `json:"confirmar"`
	}
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if !p.Confirmar {
		responderErro(w, http.StatusBadRequest, "envie confirmar: true")
		return
	}
	if err := s.cfg.AtendimentoWhatsapp.Desconectar(r.Context(), slug); err != nil {
		s.responderErroDoWorkerDeAtendimento(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "atendimento_whatsapp_desconectado", "bot", slug, nil)
	responderJSON(w, http.StatusOK, map[string]any{"desconectado": true})
}

// responderErroDoWorkerDeAtendimento traduz o que o worker disse em algo que a tela explica.
func (s *Servidor) responderErroDoWorkerDeAtendimento(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, atendimentowa.ErrDesconectado):
		responderErro(w, http.StatusConflict, "o WhatsApp deste bot não está conectado: pareie o número primeiro")
	case errors.Is(err, atendimentowa.ErrSemSessao):
		responderErro(w, http.StatusConflict, "o serviço ainda não abriu sessão para este bot (ele precisa estar ligado)")
	case errors.Is(err, atendimentowa.ErrIndisponivel):
		responderErro(w, http.StatusServiceUnavailable, "o serviço de WhatsApp do atendimento não está respondendo")
	default:
		erroInterno(w, r, err)
	}
}

// estadoNaTela traduz o estado do worker para o vocabulário que as outras telas de WhatsApp da
// plataforma já usam, para as três não divergirem em rótulo e cor.
func estadoNaTela(doWorker string) string {
	switch doWorker {
	case "aberta":
		return "conectado"
	case "qr":
		return "aguardando_qr"
	case "conectando":
		return "conectando"
	default:
		return "desconectado"
	}
}

// mascararTelefone deixa o telefone reconhecível sem gravar o número inteiro na auditoria.
func mascararTelefone(telefone string) string {
	d := reSoDigitos.ReplaceAllString(telefone, "")
	if len(d) <= 6 {
		return "***"
	}
	return d[:4] + strings.Repeat("*", len(d)-6) + d[len(d)-2:]
}
