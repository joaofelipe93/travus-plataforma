package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/auth"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const mensagemSemSessao = "sessão ausente ou expirada: entre novamente"

var errSemSessao = errors.New("sem sessão válida")

// UsuarioSessao é o usuário da requisição, depois de validar o cookie.
type UsuarioSessao struct {
	ID        int64
	Email     string
	Nome      string
	Perfil    db.PerfilUsuario
	CSRF      string
	tokenHash []byte
}

type handlerUsuario func(http.ResponseWriter, *http.Request, *UsuarioSessao)

type usuarioJSON struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Nome   string `json:"nome"`
	Perfil string `json:"perfil"`
}

type respostaSessao struct {
	Usuario   usuarioJSON `json:"usuario"`
	CSRFToken string      `json:"csrf_token"`
}

func (s *Servidor) nomeCookie() string {
	if s.cfg.CookieSecure {
		return "__Host-travus_sessao"
	}
	return "travus_sessao"
}

func (s *Servidor) cookieSessao(valor string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     s.nomeCookie(),
		Value:    valor,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
}

// origemPermitida recusa requisições que alteram dados vindas de outra origem.
// Sem Origin (curl, ferramentas) passa: nesses casos a proteção é o token CSRF.
func (s *Servidor) origemPermitida(r *http.Request) bool {
	origem := r.Header.Get("Origin")
	return origem == "" || origem == s.cfg.AppOrigin
}

func (s *Servidor) sessaoDaRequisicao(r *http.Request) (*UsuarioSessao, error) {
	c, err := r.Cookie(s.nomeCookie())
	if err != nil || c.Value == "" {
		return nil, errSemSessao
	}
	if s.q == nil {
		return nil, errSemSessao
	}
	hash := auth.HashToken(c.Value)
	agora := s.agora()
	row, err := s.q.BuscarSessaoValida(r.Context(), db.BuscarSessaoValidaParams{
		TokenHash:  hash,
		CriadaApos: agora.Add(-s.cfg.SessaoMaxima),
		UsadaApos:  agora.Add(-s.cfg.SessaoInatividade),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errSemSessao
	}
	if err != nil {
		return nil, err
	}
	// Renova o "último uso" no máximo uma vez por minuto.
	if agora.Sub(row.UltimoUsoEm) > time.Minute {
		if err := s.q.TocarSessao(r.Context(), hash); err != nil {
			slog.Warn("sessão: falha ao renovar último uso", "erro", err)
		}
	}
	return &UsuarioSessao{ID: row.UsuarioID, Email: row.Email, Nome: row.Nome, Perfil: row.Perfil, CSRF: row.CsrfToken, tokenHash: hash}, nil
}

// autenticado exige sessão válida e, em métodos que alteram dados, Origin permitida e
// X-CSRF-Token igual ao da sessão.
func (s *Servidor) autenticado(h handlerUsuario) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.sessaoDaRequisicao(r)
		if err != nil {
			if !errors.Is(err, errSemSessao) {
				erroInterno(w, r, err)
				return
			}
			responderErro(w, http.StatusUnauthorized, mensagemSemSessao)
			return
		}
		if metodoAltera(r.Method) {
			if !s.origemPermitida(r) {
				responderErro(w, http.StatusForbidden, "origem não permitida")
				return
			}
			if !auth.CSRFConfere(r.Header.Get("X-CSRF-Token"), u.CSRF) {
				responderErro(w, http.StatusForbidden, "token CSRF ausente ou inválido: recarregue a página")
				return
			}
		}
		h(w, r, u)
	})
}

func exigirPerfil(h handlerUsuario, perfis ...db.PerfilUsuario) handlerUsuario {
	return func(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
		for _, p := range perfis {
			if u.Perfil == p {
				h(w, r, u)
				return
			}
		}
		responderErro(w, http.StatusForbidden, "seu perfil não permite esta ação")
	}
}

type pedidoLogin struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

func (s *Servidor) login(w http.ResponseWriter, r *http.Request) {
	if !s.origemPermitida(r) {
		responderErro(w, http.StatusForbidden, "origem não permitida")
		return
	}
	var p pedidoLogin
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "envie e-mail e senha")
		return
	}
	ctx := r.Context()
	email := strings.TrimSpace(p.Email)
	const invalido = "e-mail ou senha inválidos"

	u, err := s.q.BuscarUsuarioPorEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.SimularVerificacao(p.Senha)
		s.auditar(ctx, r, nil, "login_falhou", "usuario", "", map[string]any{"email": email, "motivo": "email_inexistente"})
		responderErro(w, http.StatusUnauthorized, invalido)
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	ok, err := auth.VerificarSenha(p.Senha, u.SenhaHash)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if !ok || !u.Ativo {
		motivo := "senha_incorreta"
		if ok {
			motivo = "usuario_inativo"
		}
		s.auditar(ctx, r, &u.ID, "login_falhou", "usuario", idTexto(u.ID), map[string]any{"email": email, "motivo": motivo})
		responderErro(w, http.StatusUnauthorized, invalido)
		return
	}

	token, hash, err := auth.NovoTokenSessao()
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	csrf, err := auth.NovoTokenCSRF()
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	agora := s.agora()
	if _, err := s.q.ApagarSessoesExpiradas(ctx, db.ApagarSessoesExpiradasParams{
		CriadaApos: agora.Add(-s.cfg.SessaoMaxima), UsadaApos: agora.Add(-s.cfg.SessaoInatividade),
	}); err != nil {
		slog.Warn("sessão: falha ao limpar expiradas", "erro", err)
	}
	if err := s.q.CriarSessao(ctx, db.CriarSessaoParams{
		TokenHash: hash, UsuarioID: u.ID, CsrfToken: csrf,
		Ip: textoOuNil(ipDaRequisicao(r)), UserAgent: textoOuNil(r.UserAgent()),
	}); err != nil {
		erroInterno(w, r, err)
		return
	}

	http.SetCookie(w, s.cookieSessao(token, int(s.cfg.SessaoMaxima.Seconds())))
	s.auditar(ctx, r, &u.ID, "login", "usuario", idTexto(u.ID), nil)
	responderJSON(w, http.StatusOK, respostaSessao{
		Usuario:   usuarioJSON{ID: u.ID, Email: u.Email, Nome: u.Nome, Perfil: string(u.Perfil)},
		CSRFToken: csrf,
	})
}

func (s *Servidor) logout(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	if err := s.q.ApagarSessao(r.Context(), u.tokenHash); err != nil {
		erroInterno(w, r, err)
		return
	}
	http.SetCookie(w, s.cookieSessao("", -1))
	s.auditar(r.Context(), r, &u.ID, "logout", "usuario", idTexto(u.ID), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Servidor) sessao(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	responderJSON(w, http.StatusOK, respostaSessao{
		Usuario:   usuarioJSON{ID: u.ID, Email: u.Email, Nome: u.Nome, Perfil: string(u.Perfil)},
		CSRFToken: u.CSRF,
	})
}

// verificar é o endereço do ForwardAuth do Traefik. 204 libera a requisição.
// Sem sessão: ?modo=pagina redireciona para /login (navegação no app); senão, 401.
func (s *Servidor) verificar(w http.ResponseWriter, r *http.Request) {
	u, err := s.sessaoDaRequisicao(r)
	if err == nil {
		w.Header().Set("X-Usuario-Perfil", string(u.Perfil))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !errors.Is(err, errSemSessao) {
		erroInterno(w, r, err)
		return
	}
	if r.URL.Query().Get("modo") == "pagina" {
		destino := s.cfg.AppOrigin + "/login"
		if uri := r.Header.Get("X-Forwarded-Uri"); caminhoDeRetornoSeguro(uri) && uri != "/" {
			destino += "?proximo=" + url.QueryEscape(uri)
		}
		// URL absoluta: o Traefik repassa o Location ao navegador.
		w.Header().Set("Location", destino)
		w.WriteHeader(http.StatusFound)
		return
	}
	responderErro(w, http.StatusUnauthorized, mensagemSemSessao)
}

// caminhoDeRetornoSeguro aceita só caminhos locais ("/cotas"), nunca "//outro-site".
func caminhoDeRetornoSeguro(caminho string) bool {
	return strings.HasPrefix(caminho, "/") && !strings.HasPrefix(caminho, "//") && !strings.HasPrefix(caminho, "/\\")
}

func (s *Servidor) auditar(ctx context.Context, r *http.Request, usuarioID *int64, acao, entidade, entidadeID string, detalhes map[string]any) {
	if err := s.q.RegistrarAuditoria(ctx, paramsAuditoria(r, usuarioID, acao, entidade, entidadeID, detalhes)); err != nil {
		slog.Error("auditoria: falha ao registrar", "acao", acao, "erro", err)
	}
}

func paramsAuditoria(r *http.Request, usuarioID *int64, acao, entidade, entidadeID string, detalhes map[string]any) db.RegistrarAuditoriaParams {
	bruto := []byte("{}")
	if len(detalhes) > 0 {
		if b, err := json.Marshal(detalhes); err == nil {
			bruto = b
		}
	}
	var ip *string
	if r != nil {
		ip = textoOuNil(ipDaRequisicao(r))
	}
	return db.RegistrarAuditoriaParams{
		UsuarioID: usuarioID, Acao: acao, Entidade: textoOuNil(entidade),
		EntidadeID: textoOuNil(entidadeID), Detalhes: bruto, Ip: ip,
	}
}
