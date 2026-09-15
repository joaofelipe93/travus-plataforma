// Package httpapi monta as rotas HTTP da API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Pinger é o que o /health precisa do banco (o *pgxpool.Pool satisfaz).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Config do servidor HTTP.
type Config struct {
	// Origem pública do app (ex.: http://app.localhost): checagem de Origin e
	// redirecionamento para /login.
	AppOrigin string
	// Cookie com Secure e prefixo __Host-. Só desligue se o navegador recusar em dev.
	CookieSecure      bool
	SessaoInatividade time.Duration
	SessaoMaxima      time.Duration
}

type Servidor struct {
	cfg   Config
	pool  *pgxpool.Pool
	q     *db.Queries
	ping  Pinger
	agora func() time.Time
}

func NovoServidor(cfg Config, pool *pgxpool.Pool) *Servidor {
	return &Servidor{cfg: cfg, pool: pool, q: db.New(pool), ping: pool, agora: time.Now}
}

// Rotas da API. No gateway, o navegador chega por app.<domínio>/api/... (o Traefik
// remove o /api) e ferramentas por api.<domínio>/... Tudo menos /health, /auth/login e
// /auth/verificar passa antes pelo ForwardAuth; mesmo assim a API valida a sessão de novo.
func (s *Servidor) Rotas() http.Handler {
	editores := []db.PerfilUsuario{db.PerfilUsuarioAdmin, db.PerfilUsuarioOperador}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.health)

	mux.HandleFunc("POST /auth/login", s.login)
	mux.HandleFunc("GET /auth/verificar", s.verificar)
	mux.Handle("GET /auth/sessao", s.autenticado(s.sessao))
	mux.Handle("POST /auth/logout", s.autenticado(s.logout))

	mux.Handle("GET /clientes", s.autenticado(s.listarClientes))
	mux.Handle("GET /clientes/{id}", s.autenticado(s.buscarCliente))
	mux.Handle("GET /cotas", s.autenticado(s.listarCotas))
	mux.Handle("GET /cotas/grupos", s.autenticado(s.listarGrupos))
	mux.Handle("PATCH /cotas/{id}", s.autenticado(exigirPerfil(s.definirCotaAtiva, editores...)))

	mux.Handle("GET /importacoes", s.autenticado(s.listarImportacoes))
	mux.Handle("POST /importacoes", s.autenticado(exigirPerfil(s.criarImportacao, editores...)))
	mux.Handle("GET /importacoes/{id}", s.autenticado(s.buscarImportacao))
	mux.Handle("POST /importacoes/{id}/aplicar", s.autenticado(exigirPerfil(s.aplicarImportacao, editores...)))
	mux.Handle("POST /importacoes/{id}/descartar", s.autenticado(exigirPerfil(s.descartarImportacao, editores...)))

	return recuperar(mux)
}

type healthResponse struct {
	Status     string `json:"status"`
	Servico    string `json:"servico"`
	Banco      string `json:"banco"`
	ViaGateway bool   `json:"via_gateway"`
	Host       string `json:"host,omitempty"`
	Horario    string `json:"horario"`
}

// health responde se a API está de pé e se alcança o Postgres. "via_gateway" indica se a
// requisição passou pelo Traefik, que sempre preenche X-Forwarded-Host.
func (s *Servidor) health(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:     "ok",
		Servico:    "api",
		Banco:      "ok",
		ViaGateway: r.Header.Get("X-Forwarded-Host") != "",
		Host:       r.Header.Get("X-Forwarded-Host"),
		Horario:    s.agora().UTC().Format(time.RFC3339),
	}
	code := http.StatusOK

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ping.Ping(ctx); err != nil {
		// O detalhe do erro fica só no log: a rota é pública.
		slog.Warn("health: banco indisponível", "erro", err)
		resp.Status, resp.Banco = "degradado", "indisponivel"
		code = http.StatusServiceUnavailable
	}

	responderJSON(w, code, resp)
}

func recuperar(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("pânico no handler", "metodo", r.Method, "caminho", r.URL.Path, "valor", v, "pilha", string(debug.Stack()))
				responderErro(w, http.StatusInternalServerError, mensagemErroInterno)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
