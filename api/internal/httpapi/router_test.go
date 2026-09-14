package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHealth(t *testing.T) {
	tests := []struct {
		name          string
		pingErr       error
		forwardedHost string
		wantCode      int
		want          healthResponse
	}{
		{
			name:          "banco ok, pelo gateway",
			forwardedHost: "api.localhost",
			wantCode:      http.StatusOK,
			want:          healthResponse{Status: "ok", Servico: "api", Banco: "ok", ViaGateway: true, Host: "api.localhost"},
		},
		{
			name:     "banco ok, direto no container",
			wantCode: http.StatusOK,
			want:     healthResponse{Status: "ok", Servico: "api", Banco: "ok"},
		},
		{
			name:     "banco fora",
			pingErr:  errors.New("connection refused"),
			wantCode: http.StatusServiceUnavailable,
			want:     healthResponse{Status: "degradado", Servico: "api", Banco: "indisponivel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			if tt.forwardedHost != "" {
				req.Header.Set("X-Forwarded-Host", tt.forwardedHost)
			}
			rec := httptest.NewRecorder()

			NewRouter(fakePinger{err: tt.pingErr}).ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, quer %d", rec.Code, tt.wantCode)
			}
			var got healthResponse
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("json inválido: %v", err)
			}
			if got.Horario == "" {
				t.Error("horario vazio")
			}
			got.Horario = ""
			if got != tt.want {
				t.Errorf("resposta = %+v, quer %+v", got, tt.want)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
		})
	}
}

func TestHealthSoAceitaGET(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /health = %d, quer 405", rec.Code)
	}
}
