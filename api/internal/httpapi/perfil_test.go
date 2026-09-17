package httpapi

// Testes de integração do "Meu perfil": precisam de Postgres (TEST_DATABASE_URL).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/credenciais"
	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Valores fictícios: o token de verdade de alguém nunca entra em teste.
const (
	chaveTrelloTeste = "chave-ficticia-0001"
	tokenTrelloTeste = "token-ficticio-abcd1234"
)

func servidorComCofre(t *testing.T) *Servidor {
	t.Helper()
	s := novoServidorTeste(t)
	cofre, err := cripto.NovoCofre(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.Cofre = cofre
	return s
}

func TestPerfilDadosDeContato(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, rec := entrar(t, h, "operador@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	rec = n.req(http.MethodGet, "/perfil", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "perfil")
	inicial := decodificar[respostaPerfil](t, rec)
	if inicial.Usuario.Email != "operador@exemplo.com" || inicial.Usuario.Perfil != "operador" {
		t.Errorf("perfil inicial: %+v", inicial.Usuario)
	}
	if inicial.Usuario.Telefone != nil || inicial.Usuario.Cargo != nil {
		t.Errorf("perfil novo devia vir sem contato: %+v", inicial.Usuario)
	}

	rec = n.enviarJSON(http.MethodPatch, "/perfil", map[string]any{
		"nome": "  Ana   Maria ", "telefone": "(11) 90000-0000", "cargo": "Operadora de lances",
		"observacoes": "Trabalha de manhã",
	})
	esperarStatus(t, rec, http.StatusOK, "atualizar perfil")
	atualizado := decodificar[perfilJSON](t, rec)
	if atualizado.Nome != "Ana Maria" {
		t.Errorf("nome = %q, quer os espaços normalizados", atualizado.Nome)
	}
	if valorOuVazio(atualizado.Telefone) != "(11) 90000-0000" || valorOuVazio(atualizado.Cargo) != "Operadora de lances" {
		t.Errorf("contato não gravado: %+v", atualizado)
	}

	// Campo ausente não mexe no que já estava; e-mail e perfil não se editam por aqui.
	rec = n.enviarJSON(http.MethodPatch, "/perfil", map[string]any{"cargo": "Operadora sênior"})
	esperarStatus(t, rec, http.StatusOK, "atualização parcial")
	parcial := decodificar[perfilJSON](t, rec)
	if valorOuVazio(parcial.Telefone) != "(11) 90000-0000" || parcial.Nome != "Ana Maria" {
		t.Errorf("atualização parcial apagou dados: %+v", parcial)
	}
	rec = n.enviarJSON(http.MethodPatch, "/perfil", map[string]any{"email": "outro@exemplo.com"})
	esperarStatus(t, rec, http.StatusBadRequest, "trocar e-mail pelo perfil")

	// Campo em branco limpa.
	rec = n.enviarJSON(http.MethodPatch, "/perfil", map[string]any{"telefone": ""})
	esperarStatus(t, rec, http.StatusOK, "limpar telefone")
	if decodificar[perfilJSON](t, rec).Telefone != nil {
		t.Error("telefone devia ter sido limpo")
	}

	for nome, corpo := range map[string]map[string]any{
		"telefone sem dígitos suficientes": {"telefone": "1234"},
		"telefone com letras":              {"telefone": "ligar para a Ana"},
		"nome vazio":                       {"nome": " "},
		"cargo longo demais":               {"cargo": strings.Repeat("x", 81)},
		"observações longas demais":        {"observacoes": strings.Repeat("x", 501)},
	} {
		esperarStatus(t, n.enviarJSON(http.MethodPatch, "/perfil", corpo), http.StatusBadRequest, nome)
	}

	if got := contarAuditoria(t, s, "perfil_atualizado"); got != 3 {
		t.Errorf("perfil_atualizado na auditoria = %d, quer 3 (mudanças de verdade)", got)
	}
}

func TestCredenciaisPessoais(t *testing.T) {
	s := servidorComCofre(t)
	h := s.Rotas()
	ctx := context.Background()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	criarUsuario(t, s, "outro@exemplo.com", db.PerfilUsuarioLeitura)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	outro, _ := entrar(t, h, "outro@exemplo.com", senhaTeste)

	rec := n.req(http.MethodGet, "/perfil", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "perfil")
	perfil := decodificar[respostaPerfil](t, rec)
	if len(perfil.Credenciais) == 0 || perfil.Credenciais[0].ID != "trello" || perfil.Credenciais[0].Cadastrada {
		t.Fatalf("catálogo devia vir com o Trello por cadastrar: %+v", perfil.Credenciais)
	}
	if !perfil.CofreConfigurado {
		t.Error("cofre configurado devia ser true")
	}

	valores := map[string]any{"valores": map[string]string{"chave": chaveTrelloTeste, "token": tokenTrelloTeste}}
	rec = n.enviarJSON(http.MethodPut, "/perfil/credenciais/trello", valores)
	esperarStatus(t, rec, http.StatusOK, "gravar credencial")
	if strings.Contains(rec.Body.String(), tokenTrelloTeste) || strings.Contains(rec.Body.String(), chaveTrelloTeste) {
		t.Fatalf("a resposta devolveu o segredo: %s", rec.Body.String())
	}
	gravada := decodificar[credencialJSON](t, rec)
	if !gravada.Cadastrada || gravada.Dica != "1234" || gravada.AtualizadaEm == nil {
		t.Errorf("credencial gravada: %+v", gravada)
	}

	// O serviço que for usar o Trello lê assim; o valor confere.
	lidos, err := credenciais.Ler(ctx, s.q, s.cfg.Cofre, perfil.Usuario.ID, "trello")
	if err != nil {
		t.Fatalf("lendo a credencial: %v", err)
	}
	if lidos["token"] != tokenTrelloTeste || lidos["chave"] != chaveTrelloTeste {
		t.Errorf("valores decifrados diferentes: %+v", lidos)
	}

	rec = n.req(http.MethodGet, "/perfil", nil, nil)
	depois := decodificar[respostaPerfil](t, rec)
	if !depois.Credenciais[0].Cadastrada || depois.Credenciais[0].Dica != "1234" {
		t.Errorf("perfil não mostra a credencial: %+v", depois.Credenciais[0])
	}
	if strings.Contains(rec.Body.String(), tokenTrelloTeste) {
		t.Fatal("o /perfil devolveu o segredo")
	}

	// Credencial é de quem cadastrou: outra pessoa não vê nem herda.
	rec = outro.req(http.MethodGet, "/perfil", nil, nil)
	if decodificar[respostaPerfil](t, rec).Credenciais[0].Cadastrada {
		t.Error("a credencial de uma pessoa apareceu no perfil de outra")
	}
	outroPerfil := decodificar[respostaPerfil](t, rec).Usuario
	if _, err := credenciais.Ler(ctx, s.q, s.cfg.Cofre, outroPerfil.ID, "trello"); !errors.Is(err, credenciais.ErrNaoCadastrada) {
		t.Errorf("ler a credencial de outra pessoa: erro %v, quer ErrNaoCadastrada", err)
	}

	esperarStatus(t, n.enviarJSON(http.MethodPut, "/perfil/credenciais/trello",
		map[string]any{"valores": map[string]string{"chave": chaveTrelloTeste}}), http.StatusBadRequest, "sem o token")
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/perfil/credenciais/nao-existe", valores), http.StatusNotFound, "credencial fora do catálogo")
	esperarStatus(t, n.req(http.MethodPut, "/perfil/credenciais/trello", strings.NewReader(`{}`),
		map[string]string{"Origin": origemTeste, "Content-Type": "application/json"}), http.StatusForbidden, "sem CSRF")

	esperarStatus(t, n.enviar(http.MethodDelete, "/perfil/credenciais/trello", nil, ""), http.StatusNoContent, "remover credencial")
	if _, err := credenciais.Ler(ctx, s.q, s.cfg.Cofre, perfil.Usuario.ID, "trello"); !errors.Is(err, credenciais.ErrNaoCadastrada) {
		t.Errorf("depois de remover: erro %v, quer ErrNaoCadastrada", err)
	}

	if contarAuditoria(t, s, "credencial_gravada") != 1 || contarAuditoria(t, s, "credencial_removida") != 1 {
		t.Error("gravar/remover credencial não foram auditados")
	}
	var detalhes string
	if err := s.pool.QueryRow(ctx, "SELECT detalhes::text FROM auditoria WHERE acao = 'credencial_gravada'").Scan(&detalhes); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detalhes, tokenTrelloTeste) {
		t.Fatalf("a auditoria guardou o segredo: %s", detalhes)
	}
}

func TestCredencialSemCofreRecusa(t *testing.T) {
	s := novoServidorTeste(t) // sem CHAVE_CRIPTOGRAFIA
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	rec := n.req(http.MethodGet, "/perfil", nil, nil)
	if decodificar[respostaPerfil](t, rec).CofreConfigurado {
		t.Error("cofre configurado devia ser false")
	}
	rec = n.enviarJSON(http.MethodPut, "/perfil/credenciais/trello",
		map[string]any{"valores": map[string]string{"chave": chaveTrelloTeste, "token": tokenTrelloTeste}})
	esperarStatus(t, rec, http.StatusServiceUnavailable, "gravar sem cofre")
}

func TestPanoramaCredenciaisSoAdmin(t *testing.T) {
	s := servidorComCofre(t)
	h := s.Rotas()
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	operador, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)

	esperarStatus(t, operador.enviarJSON(http.MethodPut, "/perfil/credenciais/trello",
		map[string]any{"valores": map[string]string{"chave": chaveTrelloTeste, "token": tokenTrelloTeste}}),
		http.StatusOK, "operador grava a sua credencial")

	esperarStatus(t, operador.req(http.MethodGet, "/credenciais", nil, nil), http.StatusForbidden, "operador no panorama")

	rec := admin.req(http.MethodGet, "/credenciais", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "panorama do admin")
	if strings.Contains(rec.Body.String(), tokenTrelloTeste) || strings.Contains(rec.Body.String(), `"dica"`) {
		t.Fatalf("o panorama mostrou segredo ou dica: %s", rec.Body.String())
	}
	panorama := decodificar[struct {
		Credenciais []credencialAdminJSON `json:"credenciais"`
	}](t, rec)
	if len(panorama.Credenciais) != 1 || len(panorama.Credenciais[0].Usuarios) != 2 {
		t.Fatalf("panorama: %+v", panorama)
	}
	porEmail := map[string]bool{}
	for _, u := range panorama.Credenciais[0].Usuarios {
		porEmail[u.Email] = u.Cadastrada
	}
	if !porEmail["operador@exemplo.com"] || porEmail["admin@exemplo.com"] {
		t.Errorf("quem tem credencial saiu errado: %+v", porEmail)
	}
}
