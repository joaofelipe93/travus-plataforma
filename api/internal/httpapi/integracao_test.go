package httpapi

// Testes de integração: precisam de Postgres (TEST_DATABASE_URL). Rode com `make test`.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/auth"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
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
		TokenWorker: tokenWorkerTeste, RetencaoScreenshots: 30 * 24 * time.Hour,
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
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/clientes", map[string]any{"nome": "X"}), http.StatusForbidden, "leitura cadastra cliente")
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/cotas", map[string]any{"cliente_id": 1, "grupo": "6650", "cota": "924"}), http.StatusForbidden, "leitura cadastra cota")
	esperarStatus(t, n.enviar(http.MethodDelete, "/clientes/1", nil, ""), http.StatusForbidden, "leitura exclui cliente")
}

// respostaCadastro é o que POST/PATCH de cliente devolvem: o cliente com as cotas dele.
type respostaCadastro struct {
	Cliente clienteJSON `json:"cliente"`
	Cotas   []cotaJSON  `json:"cotas"`
}

func (n *navegador) cadastrarCliente(t *testing.T, corpo map[string]any) respostaCadastro {
	t.Helper()
	rec := n.enviarJSON(http.MethodPost, "/clientes", corpo)
	esperarStatus(t, rec, http.StatusCreated, "cadastrar cliente")
	return decodificar[respostaCadastro](t, rec)
}

// cotaFicticia: os campos de uma cota como o formulário do CRM manda.
func cotaFicticia(grupo, cota string, extras map[string]any) map[string]any {
	c := map[string]any{
		"grupo": grupo, "cota": cota, "tipo_consorcio": "IMÓVEL",
		"vendedor": "VENDEDOR EXEMPLO", "forma_pagamento": "BOLETO",
		"vencimento_parcela": 15, "dia_assembleia": 15,
	}
	for k, v := range extras {
		c[k] = v
	}
	return c
}

// cadastroFicticio repõe pelo CRM o cenário que a planilha de exemplo criava: 3 clientes e
// 4 cotas (duas do mesmo cliente).
func cadastroFicticio(t *testing.T, n *navegador) {
	t.Helper()
	n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE EXEMPLO UM", "telefone": "(11) 90000-0000", "email": "cliente.um@exemplo.com",
		"cotas": []map[string]any{
			cotaFicticia("1234", "56", map[string]any{"contratacao": "2025-10-01"}),
			cotaFicticia("1234", "89", map[string]any{"contratacao": "2025-10-01"}),
		},
	})
	n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE EXEMPLO DOIS",
		"cotas": []map[string]any{
			cotaFicticia("5678", "12", map[string]any{"tipo_consorcio": "AUTOMÓVEL"}),
		},
	})
	n.cadastrarCliente(t, map[string]any{
		"nome": "EXEMPLO, CLIENTE TRÊS",
		"cotas": []map[string]any{
			cotaFicticia("6.650", "0924", map[string]any{
				"forma_pagamento": "PIX", "vencimento_parcela": 20, "dia_assembleia": 20,
			}),
		},
	})
}

func TestCadastrarClienteComCotas(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	criado := n.cadastrarCliente(t, map[string]any{
		"nome": "  cliente   exemplo um  ", "telefone": "(11) 90000-0000", "email": "cliente.um@exemplo.com",
		"cotas": []map[string]any{
			cotaFicticia("6.650", "924", map[string]any{"contratacao": "2025-10-01"}),
			cotaFicticia("8100", "1835", map[string]any{"tipo_consorcio": "automóvel", "modalidade_padrao": "livre", "ativa": false}),
		},
	})
	if criado.Cliente.Nome != "cliente exemplo um" {
		t.Errorf("nome guardado = %q (espaços deviam virar um só)", criado.Cliente.Nome)
	}
	if criado.Cliente.Origem != "cadastro" {
		t.Errorf("origem = %q, quer cadastro", criado.Cliente.Origem)
	}
	if len(criado.Cotas) != 2 {
		t.Fatalf("cotas criadas = %d, quer 2", len(criado.Cotas))
	}
	primeira := criado.Cotas[0]
	if primeira.Grupo != "006650" || primeira.Cota != "0924" || primeira.Versao != "00" {
		t.Errorf("zeros à esquerda: %+v", primeira)
	}
	if primeira.Administradora != "CANOPUS" || primeira.ModalidadePadrao != "segundo_fixo" || !primeira.Ativa {
		t.Errorf("padrões da cota: %+v", primeira)
	}
	if primeira.Contratacao == nil || *primeira.Contratacao != "2025-10-01" {
		t.Errorf("contratação: %v", primeira.Contratacao)
	}
	if primeira.VencimentoParcela == nil || *primeira.VencimentoParcela != 15 ||
		primeira.DiaAssembleia == nil || *primeira.DiaAssembleia != 15 ||
		primeira.FormaPagamento == nil || *primeira.FormaPagamento != "BOLETO" ||
		primeira.Vendedor == nil || *primeira.Vendedor != "VENDEDOR EXEMPLO" {
		t.Errorf("campos da planilha na cota: %+v", primeira)
	}
	segunda := criado.Cotas[1]
	if segunda.ModalidadePadrao != "livre" || segunda.Ativa ||
		segunda.TipoConsorcio == nil || *segunda.TipoConsorcio != "AUTOMÓVEL" {
		t.Errorf("segunda cota: %+v", segunda)
	}

	detalhe := decodificar[respostaCadastro](t, n.req(http.MethodGet, fmt.Sprintf("/clientes/%d", criado.Cliente.ID), nil, nil))
	if len(detalhe.Cotas) != 2 {
		t.Errorf("cotas no detalhe = %d, quer 2", len(detalhe.Cotas))
	}
	if contarAuditoria(t, s, "cliente_criado") != 1 {
		t.Error("cadastro não registrado na auditoria")
	}

	// Mesmo nome com outra grafia: é o mesmo cliente (a planilha nunca teve CPF).
	repetido := n.enviarJSON(http.MethodPost, "/clientes", map[string]any{"nome": "Cliente Exemplo Um"})
	esperarStatus(t, repetido, http.StatusConflict, "cliente repetido")

	// Cota que já existe, mesmo em outro cliente.
	outro := n.enviarJSON(http.MethodPost, "/clientes", map[string]any{
		"nome":  "CLIENTE EXEMPLO DOIS",
		"cotas": []map[string]any{cotaFicticia("6650", "0924", nil)},
	})
	esperarStatus(t, outro, http.StatusConflict, "cota repetida")
	clientes := decodificar[struct{ Clientes []clienteResumoJSON }](t, n.req(http.MethodGet, "/clientes", nil, nil)).Clientes
	if len(clientes) != 1 {
		t.Errorf("a transação devia ter desfeito o cliente da cota repetida: %d clientes", len(clientes))
	}
}

func TestCadastroRecusaDadoInvalido(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	casos := map[string]map[string]any{
		"sem nome":            {"nome": "   "},
		"e-mail sem arroba":   {"nome": "A", "email": "nao-e-email"},
		"grupo sem número":    {"nome": "A", "cotas": []map[string]any{{"grupo": "abc", "cota": "12"}}},
		"grupo zerado":        {"nome": "A", "cotas": []map[string]any{{"grupo": "0", "cota": "12"}}},
		"cota grande demais":  {"nome": "A", "cotas": []map[string]any{{"grupo": "6650", "cota": "123456"}}},
		"dia fora do mês":     {"nome": "A", "cotas": []map[string]any{{"grupo": "6650", "cota": "12", "dia_assembleia": 32}}},
		"data em outro forma": {"nome": "A", "cotas": []map[string]any{{"grupo": "6650", "cota": "12", "contratacao": "01/10/2025"}}},
		"modalidade inexis.":  {"nome": "A", "cotas": []map[string]any{{"grupo": "6650", "cota": "12", "modalidade_padrao": "turbo"}}},
		"cota duas vezes": {"nome": "A", "cotas": []map[string]any{
			{"grupo": "6650", "cota": "12"}, {"grupo": "006650", "cota": "0012"},
		}},
	}
	for nome, corpo := range casos {
		esperarStatus(t, n.enviarJSON(http.MethodPost, "/clientes", corpo), http.StatusBadRequest, nome)
	}
	clientes := decodificar[struct{ Clientes []clienteResumoJSON }](t, n.req(http.MethodGet, "/clientes", nil, nil)).Clientes
	if len(clientes) != 0 {
		t.Errorf("nenhum cadastro devia ter passado: %+v", clientes)
	}
}

func TestEditarCliente(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	um := n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE UM", "telefone": "(11) 90000-0000", "email": "um@exemplo.com"})
	n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE DOIS"})
	caminho := fmt.Sprintf("/clientes/%d", um.Cliente.ID)

	// No CRM o formulário manda tudo: campo em branco apaga o contato.
	rec := n.enviarJSON(http.MethodPatch, caminho, map[string]any{"nome": "CLIENTE UM E MEIO", "telefone": "", "email": "novo@exemplo.com"})
	esperarStatus(t, rec, http.StatusOK, "editar cliente")
	editado := decodificar[respostaCadastro](t, rec).Cliente
	if editado.Nome != "CLIENTE UM E MEIO" || editado.Telefone != nil || editado.Email == nil || *editado.Email != "novo@exemplo.com" {
		t.Errorf("cliente editado: %+v", editado)
	}
	if contarAuditoria(t, s, "cliente_editado") != 1 {
		t.Error("edição não auditada")
	}

	esperarStatus(t, n.enviarJSON(http.MethodPatch, caminho, map[string]any{"nome": "cliente dois"}), http.StatusConflict, "nome de outro cliente")
	esperarStatus(t, n.enviarJSON(http.MethodPatch, "/clientes/999999", map[string]any{"nome": "X"}), http.StatusNotFound, "cliente inexistente")
	esperarStatus(t, n.enviarJSON(http.MethodPatch, caminho, map[string]any{"nome": "X", "cotas": []map[string]any{{"grupo": "6650", "cota": "1"}}}),
		http.StatusBadRequest, "cotas no PATCH do cliente")
}

func TestCadastrarEditarEExcluirCota(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	um := n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE UM"})
	dois := n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE DOIS"})

	rec := n.enviarJSON(http.MethodPost, "/cotas", cotaFicticia("6650", "924", map[string]any{"cliente_id": um.Cliente.ID}))
	esperarStatus(t, rec, http.StatusCreated, "criar cota")
	cota := decodificar[struct {
		Cota cotaJSON `json:"cota"`
	}](t, rec).Cota
	if cota.ClienteID != um.Cliente.ID || cota.Grupo != "006650" || cota.Cota != "0924" {
		t.Fatalf("cota criada: %+v", cota)
	}
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/cotas", cotaFicticia("6650", "924", map[string]any{"cliente_id": dois.Cliente.ID})),
		http.StatusConflict, "cota repetida em outro cliente")
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/cotas", cotaFicticia("6650", "1", map[string]any{"cliente_id": 999999})),
		http.StatusNotFound, "cliente inexistente")
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/cotas", cotaFicticia("6650", "1", nil)), http.StatusBadRequest, "cota sem cliente")

	// Editar: muda modalidade, contato da planilha e o dono.
	caminho := fmt.Sprintf("/cotas/%d", cota.ID)
	rec = n.enviarJSON(http.MethodPut, caminho, cotaFicticia("6650", "924", map[string]any{
		"cliente_id": dois.Cliente.ID, "modalidade_padrao": "fixo", "vendedor": "OUTRO VENDEDOR", "contratacao": "2024-12-13",
	}))
	esperarStatus(t, rec, http.StatusOK, "editar cota")
	editada := decodificar[struct {
		Cota cotaJSON `json:"cota"`
	}](t, rec).Cota
	if editada.ClienteID != dois.Cliente.ID || editada.ModalidadePadrao != "fixo" ||
		editada.Vendedor == nil || *editada.Vendedor != "OUTRO VENDEDOR" ||
		editada.Contratacao == nil || *editada.Contratacao != "2024-12-13" {
		t.Errorf("cota editada: %+v", editada)
	}
	if contarAuditoria(t, s, "cota_editada") != 1 {
		t.Error("edição da cota não auditada")
	}

	// Cota usada numa execução: dá para mudar os dados, não a identidade nem o dono.
	n.criarDryRun(t, []int64{cota.ID})
	rec = n.enviarJSON(http.MethodPut, caminho, cotaFicticia("6650", "925", map[string]any{"cliente_id": dois.Cliente.ID}))
	esperarStatus(t, rec, http.StatusConflict, "mudar identidade de cota com histórico")
	if !strings.Contains(rec.Body.String(), "histórico") {
		t.Errorf("mensagem: %s", rec.Body.String())
	}
	esperarStatus(t, n.enviarJSON(http.MethodPut, caminho, cotaFicticia("6650", "924", map[string]any{
		"cliente_id": dois.Cliente.ID, "modalidade_padrao": "limitado",
	})), http.StatusOK, "mudar só os dados de cota com histórico")

	esperarStatus(t, n.enviar(http.MethodDelete, caminho, nil, ""), http.StatusConflict, "excluir cota com histórico")

	// Cota sem histórico: exclui.
	nova := decodificar[struct {
		Cota cotaJSON `json:"cota"`
	}](t, n.enviarJSON(http.MethodPost, "/cotas", cotaFicticia("6650", "2068", map[string]any{"cliente_id": um.Cliente.ID}))).Cota
	esperarStatus(t, n.enviar(http.MethodDelete, fmt.Sprintf("/cotas/%d", nova.ID), nil, ""), http.StatusNoContent, "excluir cota")
	esperarStatus(t, n.enviar(http.MethodDelete, fmt.Sprintf("/cotas/%d", nova.ID), nil, ""), http.StatusNotFound, "excluir de novo")
	if contarAuditoria(t, s, "cota_excluida") != 1 {
		t.Error("exclusão da cota não auditada")
	}

	// Cliente com cota não se exclui; sem cota, sim.
	esperarStatus(t, n.enviar(http.MethodDelete, fmt.Sprintf("/clientes/%d", dois.Cliente.ID), nil, ""), http.StatusConflict, "excluir cliente com cota")
	esperarStatus(t, n.enviar(http.MethodDelete, fmt.Sprintf("/clientes/%d", um.Cliente.ID), nil, ""), http.StatusNoContent, "excluir cliente sem cota")
	if contarAuditoria(t, s, "cliente_excluido") != 1 {
		t.Error("exclusão do cliente não auditada")
	}
}

// A importação de planilha saiu com o CRM: o histórico continua legível, escrever não.
func TestImportacaoSoLeitura(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	esperarStatus(t, n.req(http.MethodGet, "/importacoes", nil, nil), http.StatusOK, "histórico de importações")
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/importacoes", map[string]any{}), http.StatusMethodNotAllowed, "importar planilha")
	esperarStatus(t, n.enviar(http.MethodPost, "/importacoes/1/aplicar", nil, ""), http.StatusNotFound, "aplicar importação")
}

func TestAtivarEDesativarCota(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	n.cadastrarCliente(t, map[string]any{"nome": "A", "cotas": []map[string]any{cotaFicticia("6650", "2068", nil)}})
	n.cadastrarCliente(t, map[string]any{"nome": "B", "cotas": []map[string]any{cotaFicticia("6650", "924", nil)}})

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
