// Package httpapi monta as rotas HTTP da API.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger é o que o /health precisa do banco (o *pgxpool.Pool satisfaz).
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewRouter registra as rotas públicas (atrás do Traefik).
func NewRouter(db Pinger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /health", Health(db, time.Now))
	return mux
}

type healthResponse struct {
	Status     string `json:"status"`
	Servico    string `json:"servico"`
	Banco      string `json:"banco"`
	ViaGateway bool   `json:"via_gateway"`
	Host       string `json:"host,omitempty"`
	Horario    string `json:"horario"`
}

// Health responde se a API está de pé e se alcança o Postgres. "via_gateway" indica se a
// requisição passou pelo Traefik, que sempre preenche X-Forwarded-Host.
func Health(db Pinger, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{
			Status:     "ok",
			Servico:    "api",
			Banco:      "ok",
			ViaGateway: r.Header.Get("X-Forwarded-Host") != "",
			Host:       r.Header.Get("X-Forwarded-Host"),
			Horario:    now().UTC().Format(time.RFC3339),
		}
		code := http.StatusOK

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			// O detalhe do erro fica só no log: a rota é pública.
			slog.Warn("health: banco indisponível", "erro", err)
			resp.Status, resp.Banco = "degradado", "indisponivel"
			code = http.StatusServiceUnavailable
		}

		writeJSON(w, code, resp)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("falha ao escrever resposta", "erro", err)
	}
}
