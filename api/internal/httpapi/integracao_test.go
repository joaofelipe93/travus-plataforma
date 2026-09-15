package httpapi

// Testes de integração: precisam de Postgres (TEST_DATABASE_URL). Rode com `make test`.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/auth"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/importacao"
	"github.com/joaofelipe93/travus-plataforma/api/internal/testedb"
)

const (
	origemTeste = "http://app.teste"
	senhaTeste  = "senha-de-teste-123"
)

func novoServidorTeste(t *testing.T) *Servidor {
	t.Helper()
	return NovoServidor(Config{
		AppOrigin: origemTeste, CookieSecure: true,
		SessaoInatividade: 12 * time.Hour, SessaoMaxima: 7 * 24 * time.Hour,
	}, testedb.Abrir(t))
}

func criarUsuario(t *testing.T, s *Servidor, email string, perfil db.PerfilUsuario) {
	t.Helper()
	hash, err := auth.GerarHashSenha(senhaTeste)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.q.CriarUsuario(context.Background(), db.CriarUsuarioParams{
		Email: email, Nome: "Pessoa " + string(perfil), SenhaHash: hash, Perfil: perfil,
	}); err != nil {
		t.Fatal(err)
	}
}

// navegador guarda o cookie e o token CSRF, como o front-end.
type navegador struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
	csrf   string
}

func (n *navegador) req(metodo, caminho string, corpo io.Reader, cabecalhos map[string]string) *httptest.ResponseRecorder {
	n.t.Helper()
	r := httptest.NewRequest(metodo, caminho, corpo)
	if n.cookie != nil {
		r.AddCookie(n.cookie)
	}
	for k, v := range cabecalhos {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	n.h.ServeHTTP(rec, r)
	return rec
}

func (n *navegador) enviar(metodo, caminho string, corpo io.Reader, contentType string) *httptest.ResponseRecorder {
	cab := map[string]string{"Origin": origemTeste, "X-CSRF-Token": n.csrf}
	if contentType != "" {
		cab["Content-Type"] = contentType
	}
	return n.req(metodo, caminho, corpo, cab)
}

func (n *navegador) enviarJSON(metodo, caminho string, v any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(v)
	return n.enviar(metodo, caminho, bytes.NewReader(b), "application/json")
}

func (n *navegador) enviarPlanilha(nome string, conteudo []byte) *httptest.ResponseRecorder {
	var corpo bytes.Buffer
	mw := multipart.NewWriter(&corpo)
	fw, _ := mw.CreateFormFile("arquivo", nome)
	_, _ = fw.Write(conteudo)
	_ = mw.Close()
	return n.enviar(http.MethodPost, "/importacoes", &corpo, mw.FormDataContentType())
}

func entrar(t *testing.T, h http.Handler, email, senha string) (*navegador, *httptest.ResponseRecorder) {
	t.Helper()
	n := &navegador{t: t, h: h}
	corpo, _ := json.Marshal(pedidoLogin{Email: email, Senha: senha})
	rec := n.req(http.MethodPost, "/auth/login", bytes.NewReader(corpo), map[string]string{"Origin": origemTeste, "Content-Type": "application/json"})
	if rec.Code == http.StatusOK {
		for _, c := range rec.Result().Cookies() {
			if c.Name == "__Host-travus_sessao" {
				n.cookie = c
			}
		}
		n.csrf = decodificar[respostaSessao](t, rec).CSRFToken
	}
	return n, rec
}

func decodificar[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("JSON inválido (HTTP %d): %s", rec.Code, rec.Body.String())
	}
	return v
}

func esperarStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, contexto string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: HTTP %d, quer %d. Corpo: %s", contexto, rec.Code, want, rec.Body.String())
	}
}

func contarAuditoria(t *testing.T, s *Servidor, acao string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM auditoria WHERE acao = $1", acao).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginSessaoELogout(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)

	// E-mail sem diferenciar maiúsculas (citext).
	n, rec := entrar(t, h, "Operador@Exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")
	c := n.cookie
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" {
		t.Fatalf("cookie de sessão inadequado: %+v", c)
	}
	if n.csrf == "" {
		t.Fatal("login sem csrf_token")
	}

	rec = n.req(http.MethodGet, "/auth/sessao", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "sessão")
	sessao := decodificar[respostaSessao](t, rec)
	if sessao.Usuario.Perfil != "operador" || sessao.CSRFToken != n.csrf {
		t.Errorf("sessão: %+v", sessao)
	}
	esperarStatus(t, n.req(http.MethodGet, "/auth/verificar", nil, nil), http.StatusNoContent, "verificar com sessão")

	esperarStatus(t, n.req(http.MethodPost, "/auth/logout", nil, map[string]string{"Origin": origemTeste}), http.StatusForbidden, "logout sem CSRF")
	esperarStatus(t, n.enviar(http.MethodPost, "/auth/logout", nil, ""), http.StatusNoContent, "logout")
	esperarStatus(t, n.req(http.MethodGet, "/auth/sessao", nil, nil), http.StatusUnauthorized, "sessão depois do logout")

	if contarAuditoria(t, s, "login") != 1 || contarAuditoria(t, s, "logout") != 1 {
		t.Error("login/logout não registrados na auditoria")
	}
}

func TestLoginRecusadoNaoRevelaMotivo(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "a@exemplo.com", db.PerfilUsuarioOperador)

	_, senhaErrada := entrar(t, h, "a@exemplo.com", "senha-errada-123")
	_, semUsuario := entrar(t, h, "ninguem@exemplo.com", senhaTeste)
	if _, err := s.q.DefinirUsuarioAtivo(context.Background(), db.DefinirUsuarioAtivoParams{Ativo: false, Email: "a@exemplo.com"}); err != nil {
		t.Fatal(err)
	}
	_, inativo := entrar(t, h, "a@exemplo.com", senhaTeste)

	for nome, rec := range map[string]*httptest.ResponseRecorder{"senha errada": senhaErrada, "sem usuário": semUsuario, "inativo": inativo} {
		esperarStatus(t, rec, http.StatusUnauthorized, nome)
		if !strings.Contains(rec.Body.String(), "e-mail ou senha inválidos") {
			t.Errorf("%s: mensagem %s", nome, rec.Body.String())
		}
	}
	if got := contarAuditoria(t, s, "login_falhou"); got != 3 {
		t.Errorf("login_falhou na auditoria = %d, quer 3", got)
	}

	corpo, _ := json.Marshal(pedidoLogin{Email: "a@exemplo.com", Senha: senhaTeste})
	rec := (&navegador{t: t, h: h}).req(http.MethodPost, "/auth/login", bytes.NewReader(corpo), map[string]string{"Origin": "http://outro.site"})
	esperarStatus(t, rec, http.StatusForbidden, "login de outra origem")
}

func TestSessaoExpira(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	ctx := context.Background()
	criarUsuario(t, s, "a@exemplo.com", db.PerfilUsuarioLeitura)

	n, _ := entrar(t, h, "a@exemplo.com", senhaTeste)
	if _, err := s.pool.Exec(ctx, "UPDATE sessoes SET ultimo_uso_em = now() - interval '13 hours'"); err != nil {
		t.Fatal(err)
	}
	esperarStatus(t, n.req(http.MethodGet, "/auth/sessao", nil, nil), http.StatusUnauthorized, "12 h sem uso")

	n, _ = entrar(t, h, "a@exemplo.com", senhaTeste)
	if _, err := s.pool.Exec(ctx, "UPDATE sessoes SET criada_em = now() - interval '8 days'"); err != nil {
		t.Fatal(err)
	}
	esperarStatus(t, n.req(http.MethodGet, "/auth/sessao", nil, nil), http.StatusUnauthorized, "mais de 7 dias")
}

func TestVerificarParaForwardAuth(t *testing.T) {
	s := novoServidorTeste(t)
	anonimo := &navegador{t: t, h: s.Rotas()}

	esperarStatus(t, anonimo.req(http.MethodGet, "/auth/verificar", nil, nil), http.StatusUnauthorized, "API sem sessão")

	rec := anonimo.req(http.MethodGet, "/auth/verificar?modo=pagina", nil, map[string]string{"X-Forwarded-Uri": "/cotas?grupo=006650"})
	esperarStatus(t, rec, http.StatusFound, "página sem sessão")
	if got, want := rec.Header().Get("Location"), origemTeste+"/login?proximo=%2Fcotas%3Fgrupo%3D006650"; got != want {
		t.Errorf("Location = %q, quer %q", got, want)
	}

	rec = anonimo.req(http.MethodGet, "/auth/verificar?modo=pagina", nil, map[string]string{"X-Forwarded-Uri": "//evil.example/x"})
	if got := rec.Header().Get("Location"); got != origemTeste+"/login" {
		t.Errorf("redirecionamento para fora do app: %q", got)
	}
}

func TestPerfilLeitura(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	n, _ := entrar(t, h, "leitura@exemplo.com", senhaTeste)

	esperarStatus(t, n.req(http.MethodGet, "/cotas", nil, nil), http.StatusOK, "leitura vê cotas")
	esperarStatus(t, n.req(http.MethodGet, "/clientes", nil, nil), http.StatusOK, "leitura vê clientes")
	esperarStatus(t, n.enviarJSON(http.MethodPatch, "/cotas/1", map[string]bool{"ativa": false}), http.StatusForbidden, "leitura desativa cota")
	esperarStatus(t, n.enviarPlanilha("x.csv", []byte("grupo,cota\n6650,924\n")), http.StatusForbidden, "leitura importa")
}

func TestImportacaoCompleta(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	planilha, err := os.ReadFile("../importacao/testdata/paridade/01-planilha-clientes.csv")
	if err != nil {
		t.Fatal(err)
	}

	rec := n.enviarPlanilha("clientes.csv", planilha)
	esperarStatus(t, rec, http.StatusCreated, "prévia")
	imp := decodificar[importacaoJSON](t, rec)
	if want := (importacao.Totais{Linhas: 4, Clientes: 3, ClientesNovos: 3, CotasNovas: 4}); imp.Previa.Totais != want {
		t.Fatalf("totais = %+v, quer %+v", imp.Previa.Totais, want)
	}
	aplicar := fmt.Sprintf("/importacoes/%d/aplicar", imp.ID)
	esperarStatus(t, n.enviar(http.MethodPost, aplicar, nil, ""), http.StatusOK, "aplicar")
	esperarStatus(t, n.enviar(http.MethodPost, aplicar, nil, ""), http.StatusConflict, "aplicar de novo")

	cotas := decodificar[struct{ Cotas []cotaJSON }](t, n.req(http.MethodGet, "/cotas", nil, nil)).Cotas
	if len(cotas) != 4 {
		t.Fatalf("cotas cadastradas = %d, quer 4", len(cotas))
	}
	clientes := decodificar[struct{ Clientes []clienteResumoJSON }](t, n.req(http.MethodGet, "/clientes", nil, nil)).Clientes
	if len(clientes) != 3 {
		t.Fatalf("clientes cadastrados = %d, quer 3", len(clientes))
	}
	busca := decodificar[struct{ Cotas []cotaJSON }](t, n.req(http.MethodGet, "/cotas?busca=6650", nil, nil)).Cotas
	if len(busca) != 1 || busca[0].Grupo != "006650" || busca[0].Cota != "0924" || busca[0].ModalidadePadrao != "segundo_fixo" {
		t.Errorf("busca por grupo: %+v", busca)
	}

	// Reimportação: sem a última linha e com telefone novo para o cliente DOIS.
	texto := strings.Replace(string(planilha), "5678, 12 ,AUTOMÓVEL,,,", "5678, 12 ,AUTOMÓVEL,,(11) 92222-2222,", 1)
	linhas := strings.Split(strings.TrimRight(texto, "\n"), "\n")
	texto = strings.Join(linhas[:len(linhas)-1], "\n") + "\n"

	rec = n.enviarPlanilha("clientes-v2.csv", []byte(texto))
	esperarStatus(t, rec, http.StatusCreated, "prévia da reimportação")
	imp2 := decodificar[importacaoJSON](t, rec)
	if want := (importacao.Totais{Linhas: 3, Clientes: 2, ClientesAlterados: 1, CotasSemMudanca: 3, ForaDaPlanilha: 1}); imp2.Previa.Totais != want {
		t.Fatalf("totais da reimportação = %+v, quer %+v", imp2.Previa.Totais, want)
	}
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/importacoes/%d/aplicar", imp2.ID), nil, ""), http.StatusOK, "aplicar reimportação")

	dois := decodificar[struct{ Clientes []clienteResumoJSON }](t, n.req(http.MethodGet, "/clientes?busca=dois", nil, nil)).Clientes
	if len(dois) != 1 || dois[0].Telefone == nil || *dois[0].Telefone != "(11) 92222-2222" {
		t.Errorf("telefone do cliente DOIS não atualizado: %+v", dois)
	}
	// A cota fora da planilha continua ativa.
	if ativas := decodificar[struct{ Cotas []cotaJSON }](t, n.req(http.MethodGet, "/cotas?ativa=true", nil, nil)).Cotas; len(ativas) != 4 {
		t.Errorf("cotas ativas = %d, quer 4", len(ativas))
	}
	if contarAuditoria(t, s, "importacao_aplicada") != 2 {
		t.Error("importações aplicadas não registradas na auditoria")
	}
}

func TestPreviaDesatualizadaEDescartar(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	planilha, _ := os.ReadFile("../importacao/testdata/paridade/01-planilha-clientes.csv")

	a := decodificar[importacaoJSON](t, n.enviarPlanilha("a.csv", planilha))
	b := decodificar[importacaoJSON](t, n.enviarPlanilha("b.csv", planilha))
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/importacoes/%d/aplicar", b.ID), nil, ""), http.StatusOK, "aplicar B")
	rec := n.enviar(http.MethodPost, fmt.Sprintf("/importacoes/%d/aplicar", a.ID), nil, "")
	esperarStatus(t, rec, http.StatusConflict, "aplicar A depois de B")
	if !strings.Contains(rec.Body.String(), "cadastro mudou") {
		t.Errorf("mensagem: %s", rec.Body.String())
	}

	descartar := fmt.Sprintf("/importacoes/%d/descartar", a.ID)
	esperarStatus(t, n.enviar(http.MethodPost, descartar, nil, ""), http.StatusOK, "descartar A")
	esperarStatus(t, n.enviar(http.MethodPost, descartar, nil, ""), http.StatusConflict, "descartar A de novo")
}

func TestImportacaoComErros(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	rec := n.enviarPlanilha("erros.csv", []byte("nome,grupo,cota\nA,6650,924\nB,0,1\n"))
	esperarStatus(t, rec, http.StatusCreated, "prévia com erro")
	imp := decodificar[importacaoJSON](t, rec)
	if imp.Previa.Totais.Erros != 1 || imp.Previa.Erros[0].Linha != 3 {
		t.Fatalf("erros: %+v", imp.Previa.Erros)
	}
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/importacoes/%d/aplicar", imp.ID), nil, ""), http.StatusUnprocessableEntity, "aplicar com erro")

	esperarStatus(t, n.enviarPlanilha("ruim.csv", []byte("nome,numero\nX,1\n")), http.StatusUnprocessableEntity, "planilha sem colunas")
}

func TestAtivarEDesativarCota(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	imp := decodificar[importacaoJSON](t, n.enviarPlanilha("c.csv", []byte("nome,grupo,cota\nA,6650,2068\nB,6650,924\n")))
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/importacoes/%d/aplicar", imp.ID), nil, ""), http.StatusOK, "aplicar")

	cota := decodificar[struct{ Cotas []cotaJSON }](t, n.req(http.MethodGet, "/cotas?busca=2068", nil, nil)).Cotas[0]
	caminho := fmt.Sprintf("/cotas/%d", cota.ID)

	esperarStatus(t, n.enviarJSON(http.MethodPatch, caminho, map[string]bool{"ativa": false}), http.StatusOK, "desativar")
	inativas := decodificar[struct{ Cotas []cotaJSON }](t, n.req(http.MethodGet, "/cotas?ativa=false", nil, nil)).Cotas
	if len(inativas) != 1 || inativas[0].ID != cota.ID {
		t.Errorf("inativas: %+v", inativas)
	}
	if contarAuditoria(t, s, "cota_desativada") != 1 {
		t.Error("desativação não auditada")
	}

	esperarStatus(t, n.enviarJSON(http.MethodPatch, caminho, map[string]string{"ativa": "talvez"}), http.StatusBadRequest, "corpo inválido")
	esperarStatus(t, n.enviarJSON(http.MethodPatch, "/cotas/999999", map[string]bool{"ativa": true}), http.StatusNotFound, "cota inexistente")
	n.csrf = "token-errado"
	esperarStatus(t, n.enviarJSON(http.MethodPatch, caminho, map[string]bool{"ativa": true}), http.StatusForbidden, "CSRF errado")
}
