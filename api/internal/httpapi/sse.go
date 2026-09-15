package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Hub escuta o NOTIFY execucao_eventos do Postgres (trigger em execucao_eventos) e acorda
// os streams SSE abertos para aquela execução. Os eventos em si são lidos do banco.
type Hub struct {
	pool      *pgxpool.Pool
	mu        sync.Mutex
	inscritos map[int64]map[chan struct{}]struct{}
}

func NovoHub(pool *pgxpool.Pool) *Hub {
	return &Hub{pool: pool, inscritos: map[int64]map[chan struct{}]struct{}{}}
}

func (h *Hub) Inscrever(execucaoID int64) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.inscritos[execucaoID] == nil {
		h.inscritos[execucaoID] = map[chan struct{}]struct{}{}
	}
	h.inscritos[execucaoID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.inscritos[execucaoID], ch)
		if len(h.inscritos[execucaoID]) == 0 {
			delete(h.inscritos, execucaoID)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) avisar(execucaoID int64, todos bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, canais := range h.inscritos {
		if !todos && id != execucaoID {
			continue
		}
		for ch := range canais {
			select {
			case ch <- struct{}{}:
			default: // já tem aviso pendente
			}
		}
	}
}

// Rodar escuta até o contexto acabar, reconectando se a conexão cair.
func (h *Hub) Rodar(ctx context.Context) {
	for ctx.Err() == nil {
		err := h.escutar(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("sse: conexão de LISTEN caiu, reconectando", "erro", err)
		h.avisar(0, true) // quem estava ouvindo busca o que perdeu
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (h *Hub) escutar(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// Conexão dedicada: fechada no fim para não voltar ao pool ainda escutando.
	defer func() {
		_ = conn.Conn().Close(context.Background())
		conn.Release()
	}()
	if _, err := conn.Exec(ctx, "LISTEN execucao_eventos"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if id, err := strconv.ParseInt(n.Payload, 10, 64); err == nil {
			h.avisar(id, false)
		}
	}
}

type eventoJSON struct {
	ID             int64           `json:"id"`
	ExecucaoCotaID *int64          `json:"execucao_cota_id"`
	Nivel          string          `json:"nivel"`
	Mensagem       string          `json:"mensagem"`
	Dados          json.RawMessage `json:"dados"`
	CriadoEm       time.Time       `json:"criado_em"`
}

// eventosExecucao é o stream SSE de uma execução. Reenvia a partir de Last-Event-ID (ou
// ?desde=) e manda "event: fim" quando a execução termina.
func (s *Servidor) eventosExecucao(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	}
	ctx := r.Context()
	if _, err := s.q.BuscarExecucao(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "execução não encontrada")
		return
	} else if err != nil {
		erroInterno(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		erroInterno(w, r, errors.New("o servidor não suporta streaming"))
		return
	}

	var ultimo int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		ultimo, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("desde"); v != "" {
		ultimo, _ = strconv.ParseInt(v, 10, 64)
	}

	sinal, sair := s.hub.Inscrever(id)
	defer sair()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	enviarNovos := func() error {
		for {
			eventos, err := s.q.ListarEventos(ctx, db.ListarEventosParams{ExecucaoID: id, AposID: ultimo, Limite: 200})
			if err != nil {
				return err
			}
			for _, e := range eventos {
				dados, _ := json.Marshal(eventoJSON{e.ID, e.ExecucaoCotaID, e.Nivel, e.Mensagem, json.RawMessage(e.Dados), e.CriadoEm})
				fmt.Fprintf(w, "id: %d\nevent: evento\ndata: %s\n\n", e.ID, dados)
				ultimo = e.ID
			}
			if len(eventos) > 0 {
				flusher.Flush()
			}
			if len(eventos) < 200 {
				return nil
			}
		}
	}

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	varredura := time.NewTicker(5 * time.Second) // rede de segurança se um NOTIFY se perder
	defer varredura.Stop()

	for {
		if err := enviarNovos(); err != nil {
			if ctx.Err() == nil {
				slog.Warn("sse: falha ao ler eventos", "execucao", id, "erro", err)
			}
			return
		}
		e, err := s.q.BuscarExecucao(ctx, id)
		if err != nil {
			return
		}
		if execucaoFinalizada(e.Status) {
			// A finalização grava evento e situação na mesma transação: esvazia de novo antes do fim.
			if err := enviarNovos(); err != nil {
				return
			}
			fmt.Fprintf(w, "event: fim\ndata: {\"status\":%q}\n\n", e.Status)
			flusher.Flush()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-s.encerrando: // API desligando: o EventSource do navegador reconecta sozinho
			return
		case <-sinal:
		case <-varredura.C:
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
