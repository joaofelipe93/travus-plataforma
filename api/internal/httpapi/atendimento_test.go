package httpapi

// Serviço de Atendimento (issue #100): o cadastro dos bots e das perguntas. O que estes
// testes guardam, além do caminho feliz, são as duas regras que protegem quem está do outro
// lado da conversa: só admin autoriza o envio de verdade, e fluxo não muda debaixo de conversa
// aberta. Precisam de Postgres (TEST_DATABASE_URL); rode com `make test`.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

func limparAtendimento(t *testing.T, s *Servidor) {
	t.Helper()
	s.exec(t, "TRUNCATE atendimento.bots, atendimento.perguntas, atendimento.conversas, atendimento.mensagens, atendimento.coletas, atendimento.leads RESTART IDENTITY CASCADE")
}

// botDeTeste devolve o corpo de um bot válido, para cada teste mexer só no que lhe interessa.
func botDeTeste(slug string) map[string]any {
	return map[string]any{
		"nome":       "Atendimento " + slug,
		"slug":       slug,
		"instrucoes": "Você é o primeiro atendimento da Travus e só afirma o que está aqui.",
	}
}

func TestAtendimentoCadastroDeBot(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	criarUsuario(t, s, "op-bot@teste", db.PerfilUsuarioOperador)
	h := s.Rotas()
	op, _ := entrar(t, h, "op-bot@teste", senhaTeste)

	// Criar
	rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("vendas"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("criar bot: %d %s", rec.Code, rec.Body)
	}
	criado := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	if criado.Modelo == "" {
		t.Error("o bot devia nascer com o modelo padrão")
	}
	if criado.Ativo {
		t.Error("bot novo precisa nascer desligado: ninguém liga sem querer")
	}

	// Slug repetido não passa: é a identidade da sessão do WhatsApp.
	if rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("vendas")); rec.Code != http.StatusConflict {
		t.Errorf("slug repetido devia dar 409, veio %d", rec.Code)
	}

	// Instrução curta é recusada pelo servidor, não só pela tela.
	curto := botDeTeste("suporte")
	curto["instrucoes"] = "responda"
	if rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", curto); rec.Code != http.StatusBadRequest {
		t.Errorf("instrução curta devia dar 400, veio %d", rec.Code)
	}

	// Atualizar: o formulário inteiro.
	corpo := botDeTeste("vendas")
	corpo["nome"] = "Atendimento de vendas"
	corpo["ativo"] = true
	corpo["max_mensagens_hora"] = 120
	rec = op.enviarJSON(http.MethodPut, "/atendimento/bots/"+idTexto(criado.ID), corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("atualizar: %d %s", rec.Code, rec.Body)
	}
	atualizado := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	if !atualizado.Ativo || atualizado.MaxMensagensHora != 120 {
		t.Errorf("atualização não valeu: %+v", atualizado)
	}
	// Trocar o slug é recusado: o pareamento do WhatsApp mora numa pasta com esse nome.
	outro := botDeTeste("vendas-2")
	if rec := op.enviarJSON(http.MethodPut, "/atendimento/bots/"+idTexto(criado.ID), outro); rec.Code != http.StatusConflict {
		t.Errorf("trocar o slug devia dar 409, veio %d", rec.Code)
	}

	// Teto fora do intervalo é 400 (o CHECK do banco diria o mesmo, mas com mensagem feia).
	ruim := botDeTeste("vendas")
	ruim["max_tokens_conversa"] = 10
	if rec := op.enviarJSON(http.MethodPut, "/atendimento/bots/"+idTexto(criado.ID), ruim); rec.Code != http.StatusBadRequest {
		t.Errorf("teto fora do intervalo devia dar 400, veio %d", rec.Code)
	}

	// Lista traz as contagens que a tela usa.
	rec = op.req(http.MethodGet, "/atendimento/bots", nil, nil)
	lista := decodificar[struct {
		Bots []botJSON `json:"bots"`
	}](t, rec).Bots
	if len(lista) != 1 || lista[0].Perguntas == nil || *lista[0].Perguntas != 0 {
		t.Errorf("lista devia trazer 1 bot com 0 perguntas: %+v", lista)
	}

	// Excluir bot sem conversa: sai.
	if rec := op.enviarJSON(http.MethodDelete, "/atendimento/bots/"+idTexto(criado.ID), nil); rec.Code != http.StatusNoContent {
		t.Errorf("excluir devia dar 204, veio %d", rec.Code)
	}
}

// O botão do bot é o único interruptor, e **admin e operador** mexem nele — quem monta o bot é
// quem o liga (decisão do usuário em 29/10/2026: "só quero um botão"). Antes havia um
// `envio_habilitado` só de admin, que deixava o bot conversar sem nada sair; eram travas demais.
func TestAtendimentoBotaoLigaEDesliga(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	criarUsuario(t, s, "op-botao@teste", db.PerfilUsuarioOperador)
	criarUsuario(t, s, "leitura-botao@teste", db.PerfilUsuarioLeitura)
	h := s.Rotas()
	op, _ := entrar(t, h, "op-botao@teste", senhaTeste)
	leitura, _ := entrar(t, h, "leitura-botao@teste", senhaTeste)

	rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("cobranca"))
	bot := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	rota := "/atendimento/bots/" + idTexto(bot.ID)

	ligar := func(n *navegador, ligado bool) *httptest.ResponseRecorder {
		corpo := botDeTeste(bot.Slug)
		corpo["ativo"] = ligado
		return n.enviarJSON(http.MethodPut, rota, corpo)
	}

	// Perfil leitura não liga nada.
	if rec := ligar(leitura, true); rec.Code != http.StatusForbidden {
		t.Errorf("leitura devia levar 403 ao ligar o bot, veio %d", rec.Code)
	}
	// Operador liga.
	rec = ligar(op, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("operador devia conseguir ligar: %d %s", rec.Code, rec.Body)
	}
	depois := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	if !depois.Ativo {
		t.Error("o bot devia ficar ligado")
	}
	// E desliga.
	rec = ligar(op, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("desligar: %d %s", rec.Code, rec.Body)
	}
	if decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot.Ativo {
		t.Error("o bot devia ficar desligado")
	}

	// Ligar e desligar ficam na auditoria: é o histórico de quem respondeu por aquilo.
	var quantas int
	if err := s.pool.QueryRow(t.Context(),
		"SELECT count(*) FROM auditoria WHERE acao = 'atendimento_bot_atualizado'").Scan(&quantas); err != nil {
		t.Fatal(err)
	}
	if quantas < 2 {
		t.Errorf("as duas mudanças deviam estar auditadas: %d", quantas)
	}
}

func TestAtendimentoPerguntas(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	criarUsuario(t, s, "op-fluxo@teste", db.PerfilUsuarioOperador)
	h := s.Rotas()
	op, _ := entrar(t, h, "op-fluxo@teste", senhaTeste)

	rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("comercial"))
	bot := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	rota := "/atendimento/bots/" + idTexto(bot.ID) + "/perguntas"

	// As perguntas vão na ordem da lista. O campo pode vir em branco: sai do texto.
	perguntas := map[string]any{"perguntas": []map[string]any{
		{"texto": "Qual é o nome da empresa?", "campo": "nome_da_empresa", "obrigatoria": true},
		{"texto": "Quantas pessoas entrariam?", "obrigatoria": true},
	}}
	rec = op.enviarJSON(http.MethodPut, rota, perguntas)
	if rec.Code != http.StatusOK {
		t.Fatalf("gravar perguntas: %d %s", rec.Code, rec.Body)
	}
	lista := decodificar[struct {
		Perguntas []perguntaJSON `json:"perguntas"`
	}](t, rec).Perguntas
	if len(lista) != 2 || lista[0].Ordem != 1 || lista[1].Ordem != 2 {
		t.Fatalf("a ordem devia sair 1 e 2, na ordem da lista: %+v", lista)
	}
	if lista[1].Campo != "quantas_pessoas_entrariam" {
		t.Errorf("o campo devia sair do texto da pergunta: %q", lista[1].Campo)
	}
	if !lista[0].Obrigatoria {
		t.Error("a pergunta foi mandada como obrigatória")
	}

	// Regravar substitui a lista inteira e renumera.
	rec = op.enviarJSON(http.MethodPut, rota, map[string]any{"perguntas": []map[string]any{
		{"texto": "Só esta pergunta agora?", "campo": "so_esta"},
	}})
	if lista := decodificar[struct {
		Perguntas []perguntaJSON `json:"perguntas"`
	}](t, rec).Perguntas; len(lista) != 1 || lista[0].Ordem != 1 {
		t.Errorf("regravar devia deixar só a pergunta nova, na ordem 1: %+v", lista)
	}

	// Duas perguntas no mesmo dado é 400: a segunda apagaria a primeira.
	if rec := op.enviarJSON(http.MethodPut, rota, map[string]any{"perguntas": []map[string]any{
		{"texto": "Qual é o seu nome?", "campo": "nome"},
		{"texto": "E o nome da empresa?", "campo": "nome"},
	}}); rec.Code != http.StatusBadRequest {
		t.Errorf("dado repetido devia dar 400, veio %d %s", rec.Code, rec.Body)
	}

	// Com conversa aberta, mudar as perguntas é recusado: mudaria o atendimento no meio.
	s.exec(t, "INSERT INTO atendimento.conversas (bot_id, telefone) VALUES ($1, '5511900000000')", bot.ID)
	if rec := op.enviarJSON(http.MethodPut, rota, perguntas); rec.Code != http.StatusConflict {
		t.Errorf("perguntas com conversa aberta devia dar 409, veio %d %s", rec.Code, rec.Body)
	}
	// E o bot com conversa não se exclui: é histórico.
	if rec := op.enviarJSON(http.MethodDelete, "/atendimento/bots/"+idTexto(bot.ID), nil); rec.Code != http.StatusConflict {
		t.Errorf("bot com conversa devia dar 409 ao excluir, veio %d", rec.Code)
	}
	// Encerrada a conversa, as perguntas voltam a ser editáveis.
	s.exec(t, "UPDATE atendimento.conversas SET status = 'encerrada' WHERE bot_id = $1", bot.ID)
	if rec := op.enviarJSON(http.MethodPut, rota, perguntas); rec.Code != http.StatusOK {
		t.Errorf("sem conversa aberta o fluxo devia gravar, veio %d %s", rec.Code, rec.Body)
	}
}

func TestAtendimentoModelos(t *testing.T) {
	s := novoServidorTeste(t)
	criarUsuario(t, s, "leitura-modelos@teste", db.PerfilUsuarioLeitura)
	h := s.Rotas()
	n, _ := entrar(t, h, "leitura-modelos@teste", senhaTeste)

	// Todo perfil vê o catálogo: é o que a tela usa para montar a escolha do modelo.
	rec := n.req(http.MethodGet, "/atendimento/modelos", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("modelos: %d", rec.Code)
	}
	resp := decodificar[struct {
		Modelos []struct {
			ID string `json:"id"`
		} `json:"modelos"`
		Padrao string `json:"padrao"`
	}](t, rec)
	if len(resp.Modelos) == 0 || resp.Padrao == "" {
		t.Errorf("catálogo vazio: %+v", resp)
	}
	// Leitura não cria bot.
	if rec := n.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("nao-vai")); rec.Code != http.StatusForbidden {
		t.Errorf("leitura devia levar 403 ao criar bot, veio %d", rec.Code)
	}
}
