package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Assistente da tela inicial (internal/assistente): qualquer perfil pergunta, e as ferramentas
// que o agente recebe dependem do perfil (assistente_ferramentas.go). A conversa fica só no
// navegador: a tela manda o histórico a cada pergunta e a resposta volta em SSE.

type ConfigAssistente struct {
	// nil: sem ANTHROPIC_API_KEY, a tela mostra que o assistente não está configurado.
	Modelo     assistente.Modelo
	NomeModelo string
	// nil: sem ASSISTENTE_DB_SENHA, não há consulta livre (as outras ferramentas funcionam).
	Consultor *assistente.Consultor
	// Perguntas por pessoa por hora (padrão 60): cada uma custa.
	PerguntasPorHora int
}

const (
	tempoRespostaAssistente = 4 * time.Minute
	pingAssistente          = 15 * time.Second
	limiteCorpoAssistente   = 256 << 10
)

// usoAssistente: uma pergunta por vez por pessoa e o limite por hora (em memória: a API é uma só).
type usoAssistente struct {
	ocupado   bool
	perguntas []time.Time
}

type limitadorAssistente struct {
	mu     sync.Mutex
	porUsu map[int64]*usoAssistente
}

func (l *limitadorAssistente) reservar(usuarioID int64, agora time.Time, porHora int) (liberar func(), motivo string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.porUsu == nil {
		l.porUsu = map[int64]*usoAssistente{}
	}
	u := l.porUsu[usuarioID]
	if u == nil {
		u = &usoAssistente{}
		l.porUsu[usuarioID] = u
	}
	if u.ocupado {
		return nil, "espere a resposta anterior terminar"
	}
	recentes := u.perguntas[:0]
	for _, t := range u.perguntas {
		if agora.Sub(t) < time.Hour {
			recentes = append(recentes, t)
		}
	}
	u.perguntas = recentes
	if len(u.perguntas) >= porHora {
		return nil, fmt.Sprintf("limite de %d perguntas por hora atingido: tente de novo daqui a pouco", porHora)
	}
	u.perguntas = append(u.perguntas, agora)
	u.ocupado = true
	return func() {
		l.mu.Lock()
		u.ocupado = false
		l.mu.Unlock()
	}, ""
}

type respostaEstadoAssistente struct {
	Disponivel    bool   `json:"disponivel"`
	Modelo        string `json:"modelo,omitempty"`
	ConsultaLivre bool   `json:"consulta_livre"`
	Reservas      bool   `json:"reservas"`
}

func (s *Servidor) estadoAssistente(w http.ResponseWriter, _ *http.Request, u *UsuarioSessao) {
	resp := respostaEstadoAssistente{Disponivel: s.cfg.Assistente.Modelo != nil, Reservas: podeVerReservas(u.Perfil)}
	if resp.Disponivel {
		resp.Modelo = s.cfg.Assistente.NomeModelo
		resp.ConsultaLivre = s.cfg.Assistente.Consultor != nil && podeVerReservas(u.Perfil)
	}
	responderJSON(w, http.StatusOK, resp)
}

type pedidoConversa struct {
	Mensagens []assistente.Mensagem `json:"mensagens"`
}

func (s *Servidor) conversarAssistente(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	cfg := s.cfg.Assistente
	if cfg.Modelo == nil {
		responderErro(w, http.StatusServiceUnavailable, "o assistente não está configurado (falta ANTHROPIC_API_KEY no servidor)")
		return
	}
	var pedido pedidoConversa
	if err := lerJSON(w, r, &pedido, limiteCorpoAssistente); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := assistente.ValidarHistorico(pedido.Mensagens); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		erroInterno(w, r, errors.New("o servidor não suporta streaming"))
		return
	}
	porHora := cfg.PerguntasPorHora
	if porHora <= 0 {
		porHora = 60
	}
	liberar, motivo := s.limiteAssistente.reservar(u.ID, s.agora(), porHora)
	if liberar == nil {
		responderErro(w, http.StatusTooManyRequests, motivo)
		return
	}
	defer liberar()

	ctx, cancel := context.WithTimeout(r.Context(), tempoRespostaAssistente)
	defer cancel()
	go func() {
		select {
		case <-s.encerrando:
			cancel()
		case <-ctx.Done():
		}
	}()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// O ping e o laço escrevem na mesma resposta.
	var escrita sync.Mutex
	enviar := func(tipo string, v any) {
		dados, err := json.Marshal(v)
		if err != nil {
			return
		}
		escrita.Lock()
		defer escrita.Unlock()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", tipo, dados)
		flusher.Flush()
	}
	pararPing := make(chan struct{})
	defer close(pararPing)
	go func() {
		t := time.NewTicker(pingAssistente)
		defer t.Stop()
		for {
			select {
			case <-pararPing:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				escrita.Lock()
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
				escrita.Unlock()
			}
		}
	}()

	ferramentas := s.ferramentasAssistente(u)
	agente := &assistente.Agente{
		Modelo:      cfg.Modelo,
		Instrucoes:  assistente.Instrucoes,
		Contexto:    assistente.Contexto(s.agora(), u.Nome, string(u.Perfil), ferramentas),
		Ferramentas: ferramentas,
	}
	uso, err := agente.Responder(ctx, pedido.Mensagens, func(e assistente.Evento) { enviar(e.Tipo, e) })

	pergunta := []rune(pedido.Mensagens[len(pedido.Mensagens)-1].Texto)
	if len(pergunta) > 300 {
		pergunta = append(pergunta[:300], '…')
	}
	detalhes := map[string]any{"pergunta": string(pergunta), "uso": uso, "modelo": cfg.NomeModelo}
	if err != nil {
		mensagem := mensagemErroAssistente(ctx, err)
		detalhes["erro"] = mensagem
		slog.Warn("assistente: falha ao responder", "usuario_id", u.ID, "erro", err)
		enviar("erro", map[string]string{"mensagem": mensagem})
	} else {
		enviar("fim", map[string]any{"parada": uso.Parada})
	}
	slog.Info("assistente: pergunta respondida", "usuario_id", u.ID, "rodadas", uso.Rodadas, "ferramentas", uso.Ferramentas,
		"tokens_entrada", uso.TokensEntrada, "tokens_saida", uso.TokensSaida, "tokens_cache_lidos", uso.TokensCacheLidos)
	// A auditoria grava mesmo com a requisição cancelada (a pessoa fechou a tela).
	s.auditar(context.WithoutCancel(r.Context()), r, &u.ID, "assistente_pergunta", "assistente", "", detalhes)
}

func mensagemErroAssistente(ctx context.Context, err error) string {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return "o assistente está com a chave da Anthropic inválida: avise o administrador"
		case apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode == 529 || apiErr.StatusCode >= 500:
			return "o serviço do assistente está sobrecarregado: tente de novo em instantes"
		case apiErr.StatusCode == http.StatusBadRequest && apiErr.Type() == "invalid_request_error":
			return "o assistente não conseguiu processar esta conversa: comece uma nova"
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "a resposta demorou demais: tente uma pergunta mais específica"
	}
	if ctx.Err() != nil {
		return "resposta interrompida"
	}
	return "o assistente falhou ao responder: tente de novo"
}

func podeVerReservas(p db.PerfilUsuario) bool {
	return p == db.PerfilUsuarioAdmin || p == db.PerfilUsuarioOperador
}
