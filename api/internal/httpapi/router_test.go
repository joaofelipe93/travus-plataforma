package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func servidorSemBanco(pingErr error) *Servidor {
	return &Servidor{ping: fakePinger{err: pingErr}, agora: time.Now}
}

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

			servidorSemBanco(tt.pingErr).Rotas().ServeHTTP(rec, req)

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
	servidorSemBanco(nil).Rotas().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /health = %d, quer 405", rec.Code)
	}
}

func TestRotasProtegidasSemCookie(t *testing.T) {
	h := servidorSemBanco(nil).Rotas()
	for _, rota := range []string{"GET /auth/sessao", "GET /cotas", "GET /clientes", "POST /importacoes", "PATCH /cotas/1"} {
		metodo, caminho, _ := cortar(rota)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s sem cookie = %d, quer 401", rota, rec.Code)
		}
	}
}

func TestCaminhoDeRetornoSeguro(t *testing.T) {
	casos := map[string]bool{
		"/cotas": true, "/cotas?grupo=006650": true, "/": true,
		"": false, "//evil.example": false, "/\\evil.example": false, "https://evil.example": false, "cotas": false,
	}
	for caminho, want := range casos {
		if got := caminhoDeRetornoSeguro(caminho); got != want {
			t.Errorf("caminhoDeRetornoSeguro(%q) = %v, quer %v", caminho, got, want)
		}
	}
}

func cortar(rota string) (string, string, bool) {
	for i := range rota {
		if rota[i] == ' ' {
			return rota[:i], rota[i+1:], true
		}
	}
	return "", rota, false
}
