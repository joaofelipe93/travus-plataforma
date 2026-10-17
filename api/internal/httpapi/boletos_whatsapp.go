package httpapi

// Tela do WhatsApp dos boletos (**só admin**): parear o número, ver a fila, mandar um teste e
// desconectar. A API repassa ao serviço pela rede interna com o BOLETOS_ADMIN_TOKEN — o
// navegador nunca vê esse token, e o QR dá acesso à conta do WhatsApp.
//
// É **outro número** do notificador de check-in: aquele publica num grupo, este manda no
// privado de cada cliente. A tela deixa isso explícito para ninguém parear o chip errado.

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/joaofelipe93/travus-plataforma/api/internal/boletoswa"
	"github.com/joaofelipe93/travus-plataforma/api/internal/mensagem"
)

func (s *Servidor) situacaoWhatsappBoletos(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	st, err := s.cfg.BoletosWhatsapp.Status(r.Context())
	if err != nil {
		// Serviço fora do ar não é erro da tela: ela mostra "indisponível" e explica.
		if errors.Is(err, boletoswa.ErrIndisponivel) {
			slog.Warn("serviço de boletos no WhatsApp indisponível", "caminho", r.URL.Path, "erro", err)
			responderJSON(w, http.StatusOK, map[string]any{
				"disponivel": false, "motivo": err.Error(), "estado": "indisponivel",
			})
			return
		}
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"disponivel":                    true,
		"estado":                        estadoWhatsapp(st.Status),
		"numero":                        st.Conta,
		"qr":                            st.QR,
		"envio_habilitado_servico":      st.EnvioServico,
		"envio_habilitado_configuracao": st.EnvioConfiguracao,
		"envio_habilitado_servidor":     s.cfg.BoletosEnvioHabilitado,
		"pausa_envio_segundos":          st.PausaSegundos,
		"fila":                          st.Fila,
	})
}

type pedidoTesteBoletos struct {
	Telefone string `json:"telefone"`
}

// testeWhatsappBoletos manda uma mensagem de teste para um número que o admin digita.
// **Vai para um telefone de verdade**: a tela avisa, e o texto diz que é teste.
func (s *Servidor) testeWhatsappBoletos(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoTesteBoletos
	if err := lerJSON(w, r, &p, 2<<10); err != nil {
		responderErro(w, http.StatusBadRequest, `envie {"telefone": "..."}`)
		return
	}
	telefone, err := mensagem.Telefone(p.Telefone)
	if err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.cfg.BoletosWhatsapp.EnviarTeste(r.Context(), telefone)
	if err != nil {
		responderErroBoletosWa(w, r, err)
		return
	}
	// O telefone não entra na auditoria: é dado de cliente (ou do próprio admin).
	s.auditar(r.Context(), r, &u.ID, "boletos_whatsapp_teste", "boletos_mensagem", idTexto(id), nil)
	responderJSON(w, http.StatusAccepted, map[string]any{"enfileirada": id})
}

type pedidoDesconectarBoletos struct {
	Confirmar bool `json:"confirmar"`
}

func (s *Servidor) desconectarWhatsappBoletos(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoDesconectarBoletos
	if err := lerJSON(w, r, &p, 2<<10); err != nil || !p.Confirmar {
		responderErro(w, http.StatusBadRequest,
			`confirme com {"confirmar": true}: desconectar apaga a sessão e exige parear o número de novo`)
		return
	}
	if err := s.cfg.BoletosWhatsapp.Desconectar(r.Context()); err != nil {
		responderErroBoletosWa(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "boletos_whatsapp_desconectado", "boletos_whatsapp", "1", nil)
	w.WriteHeader(http.StatusNoContent)
}

func responderErroBoletosWa(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, boletoswa.ErrDesconectado):
		responderErro(w, http.StatusConflict, "o WhatsApp dos boletos não está conectado: pareie o número primeiro")
	case errors.Is(err, boletoswa.ErrIndisponivel):
		slog.Warn("serviço de boletos no WhatsApp indisponível", "caminho", r.URL.Path, "erro", err)
		responderErro(w, http.StatusServiceUnavailable,
			"o serviço que manda os boletos no WhatsApp não respondeu: confira se o container está no ar")
	default:
		responderErro(w, http.StatusBadRequest, err.Error())
	}
}
