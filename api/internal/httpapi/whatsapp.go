package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/joaofelipe93/travus-plataforma/api/internal/checkin"
)

// Tela WhatsApp (só admin): conexão do notificador de check-in, QR, grupo de destino,
// mensagem de teste e desconectar. A API repassa ao serviço pela rede interna com o
// CHECKIN_ADMIN_TOKEN; o navegador nunca fala com o notificador nem vê o token.

const (
	mensagemNotificadorIndisponivel = "o notificador de check-in não respondeu: veja os logs do serviço checkin-whatsapp"
	mensagemWhatsappDesconectado    = "o WhatsApp não está conectado: escaneie o QR antes"
)

type respostaWhatsapp struct {
	Disponivel bool           `json:"disponivel"`
	Motivo     string         `json:"motivo,omitempty"`
	Estado     string         `json:"estado"`
	Numero     *string        `json:"numero"`
	QR         *string        `json:"qr"`
	Grupo      *checkin.Grupo `json:"grupo"`
	Fila       map[string]int `json:"fila"`
}

type grupoWhatsapp struct {
	JID           string `json:"jid"`
	Nome          string `json:"nome"`
	Participantes int    `json:"participantes"`
}

// estadoWhatsapp traduz o estado do Baileys para a tela.
func estadoWhatsapp(status string) string {
	switch status {
	case "open":
		return "conectado"
	case "qr":
		return "aguardando_qr"
	case "connecting":
		return "conectando"
	default:
		return "desconectado"
	}
}

// numeroDaConta: "5511900000000:3@s.whatsapp.net" → "5511900000000".
func numeroDaConta(conta *string) *string {
	if conta == nil || *conta == "" {
		return nil
	}
	numero, _, _ := strings.Cut(*conta, "@")
	numero, _, _ = strings.Cut(numero, ":")
	return &numero
}

// situacaoWhatsapp é consultada a cada 2 s pela tela enquanto o QR está aberto: o
// notificador fora do ar vira "indisponível" (200), não erro.
func (s *Servidor) situacaoWhatsapp(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	st, err := s.cfg.Checkin.Status(r.Context())
	if err != nil {
		slog.Debug("notificador de check-in sem resposta", "erro", err)
		responderJSON(w, http.StatusOK, respostaWhatsapp{
			Motivo: mensagemNotificadorIndisponivel, Estado: "indisponivel", Fila: map[string]int{},
		})
		return
	}
	fila := st.Fila
	if fila == nil {
		fila = map[string]int{}
	}
	responderJSON(w, http.StatusOK, respostaWhatsapp{
		Disponivel: true, Estado: estadoWhatsapp(st.Status), Numero: numeroDaConta(st.Conta),
		QR: st.QR, Grupo: st.Grupo, Fila: fila,
	})
}

func (s *Servidor) gruposWhatsapp(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	grupos, err := s.cfg.Checkin.Grupos(r.Context())
	if err != nil {
		s.responderErroWhatsapp(w, r, err)
		return
	}
	lista := make([]grupoWhatsapp, 0, len(grupos))
	for _, g := range grupos {
		lista = append(lista, grupoWhatsapp{JID: g.JID, Nome: g.Nome, Participantes: g.Participantes})
	}
	responderJSON(w, http.StatusOK, map[string]any{"grupos": lista})
}

type pedidoGrupoWhatsapp struct {
	JID string `json:"jid"`
}

func (s *Servidor) definirGrupoWhatsapp(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoGrupoWhatsapp
	if err := lerJSON(w, r, &p, 4<<10); err != nil || p.JID == "" {
		responderErro(w, http.StatusBadRequest, "informe o grupo (jid)")
		return
	}
	grupo, err := s.cfg.Checkin.DefinirGrupo(r.Context(), p.JID)
	if err != nil {
		s.responderErroWhatsapp(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "whatsapp_grupo_definido", "integracao", "whatsapp", map[string]any{
		"jid": grupo.JID, "nome": valorOuVazio(grupo.Nome),
	})
	responderJSON(w, http.StatusOK, map[string]any{"grupo": grupo})
}

func (s *Servidor) testeWhatsapp(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	grupo, err := s.cfg.Checkin.EnviarTeste(r.Context())
	if err != nil {
		s.responderErroWhatsapp(w, r, err)
		return
	}
	detalhes := map[string]any{}
	if grupo != nil {
		detalhes["jid"] = grupo.JID
	}
	s.auditar(r.Context(), r, &u.ID, "whatsapp_teste_enviado", "integracao", "whatsapp", detalhes)
	responderJSON(w, http.StatusOK, map[string]any{"grupo": grupo})
}

type pedidoDesconectarWhatsapp struct {
	Confirmar bool `json:"confirmar"`
}

// desconectarWhatsapp derruba as notificações até alguém escanear o QR novo: a tela pede
// confirmação e a API exige "confirmar": true.
func (s *Servidor) desconectarWhatsapp(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoDesconectarWhatsapp
	if err := lerJSON(w, r, &p, 1<<10); err != nil || !p.Confirmar {
		responderErro(w, http.StatusUnprocessableEntity, "confirme a desconexão: as notificações param até escanear o QR novo")
		return
	}
	if err := s.cfg.Checkin.Desconectar(r.Context()); err != nil {
		s.responderErroWhatsapp(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "whatsapp_desconectado", "integracao", "whatsapp", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Servidor) responderErroWhatsapp(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, checkin.ErrDesconectado):
		responderErro(w, http.StatusConflict, mensagemWhatsappDesconectado)
	case errors.Is(err, checkin.ErrGrupoNaoEncontrado):
		responderErro(w, http.StatusUnprocessableEntity, "o número conectado não participa deste grupo")
	case errors.Is(err, checkin.ErrSemGrupo):
		responderErro(w, http.StatusUnprocessableEntity, "escolha o grupo antes de enviar a mensagem de teste")
	case errors.Is(err, checkin.ErrIndisponivel):
		slog.Warn("notificador de check-in indisponível", "caminho", r.URL.Path, "erro", err)
		responderErro(w, http.StatusServiceUnavailable, mensagemNotificadorIndisponivel)
	default:
		erroInterno(w, r, err)
	}
}
