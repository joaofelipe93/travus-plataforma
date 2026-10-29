package httpapi

// Serviço de Atendimento (issue #100), etapa A: o cadastro dos bots e do fluxo. O que estes
// testes guardam, além do caminho feliz, são as duas regras que protegem quem está do outro
// lado da conversa: só admin autoriza o envio de verdade, e fluxo não muda debaixo de conversa
// aberta. Precisam de Postgres (TEST_DATABASE_URL); rode com `make test`.

import (
	"net/http"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

func limparAtendimento(t *testing.T, s *Servidor) {
	t.Helper()
	s.exec(t, "TRUNCATE atendimento.bots, atendimento.etapas, atendimento.conversas, atendimento.mensagens, atendimento.coletas RESTART IDENTITY CASCADE")
}

// botDeTeste devolve o corpo de um bot válido, para cada teste mexer só no que lhe interessa.
func botDeTeste(slug string) map[string]any {
	return map[string]any{
		"nome":       "Atendimento " + slug,
		"slug":       slug,
		"instrucoes": "Você atende a imobiliária, responde sobre aluguel e nunca fala de preço de venda.",
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
	if criado.Ativo || criado.EnvioHabilitado {
		t.Error("bot novo precisa nascer desligado e sem envio: ninguém liga sem querer")
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
	if atualizado.EnvioHabilitado {
		t.Error("atualizar o bot não pode ligar o envio: isso é rota de admin")
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
	if len(lista) != 1 || lista[0].Etapas == nil || *lista[0].Etapas != 0 {
		t.Errorf("lista devia trazer 1 bot com 0 etapas: %+v", lista)
	}

	// Excluir bot sem conversa: sai.
	if rec := op.enviarJSON(http.MethodDelete, "/atendimento/bots/"+idTexto(criado.ID), nil); rec.Code != http.StatusNoContent {
		t.Errorf("excluir devia dar 204, veio %d", rec.Code)
	}
}

func TestAtendimentoEnvioSoAdmin(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	criarUsuario(t, s, "op-envio@teste", db.PerfilUsuarioOperador)
	criarUsuario(t, s, "admin-envio@teste", db.PerfilUsuarioAdmin)
	h := s.Rotas()
	op, _ := entrar(t, h, "op-envio@teste", senhaTeste)
	adm, _ := entrar(t, h, "admin-envio@teste", senhaTeste)

	rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("cobranca"))
	bot := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	rota := "/atendimento/bots/" + idTexto(bot.ID) + "/envio"

	// Operador monta o bot, mas não autoriza a plataforma a falar com cliente.
	if rec := op.enviarJSON(http.MethodPut, rota, map[string]any{"envio_habilitado": true}); rec.Code != http.StatusForbidden {
		t.Errorf("operador devia levar 403 ao ligar o envio, veio %d %s", rec.Code, rec.Body)
	}
	// Sem `:=` aqui de propósito: dentro do if a variável seria outra, e o decodificar abaixo
	// leria a resposta da criação do bot em vez desta.
	rec = adm.enviarJSON(http.MethodPut, rota, map[string]any{"envio_habilitado": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin devia conseguir ligar: %d %s", rec.Code, rec.Body)
	}
	depois := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	if !depois.EnvioHabilitado {
		t.Error("o envio devia ficar ligado")
	}
	// Quem ligou fica gravado, e desligar não apaga esse registro.
	linha, err := s.q.BuscarBot(t.Context(), bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linha.EnvioHabilitadoPor == nil || linha.EnvioHabilitadoEm == nil {
		t.Error("ligar o envio devia gravar quem ligou e quando")
	}
	if rec := adm.enviarJSON(http.MethodPut, rota, map[string]any{"envio_habilitado": false}); rec.Code != http.StatusOK {
		t.Fatalf("desligar: %d", rec.Code)
	}
	if linha, _ := s.q.BuscarBot(t.Context(), bot.ID); linha.EnvioHabilitadoPor == nil {
		t.Error("desligar não devia apagar quem havia ligado: é o histórico útil")
	}
}

func TestAtendimentoFluxo(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	criarUsuario(t, s, "op-fluxo@teste", db.PerfilUsuarioOperador)
	h := s.Rotas()
	op, _ := entrar(t, h, "op-fluxo@teste", senhaTeste)

	rec := op.enviarJSON(http.MethodPost, "/atendimento/bots", botDeTeste("imoveis"))
	bot := decodificar[struct {
		Bot botJSON `json:"bot"`
	}](t, rec).Bot
	rota := "/atendimento/bots/" + idTexto(bot.ID) + "/etapas"

	fluxo := map[string]any{"etapas": []map[string]any{
		{"nome": "Interesse", "objetivo": "Descobrir o que a pessoa procura",
			"campos": []map[string]any{{"id": "interesse", "rotulo": "Interesse", "obrigatorio": true}}},
		{"nome": "Contato", "objetivo": "Pegar nome e telefone para o corretor ligar",
			"campos": []map[string]any{{"id": "nome", "rotulo": "Nome", "obrigatorio": true}, {"id": "telefone", "rotulo": "Telefone", "obrigatorio": true}}},
	}}
	rec = op.enviarJSON(http.MethodPut, rota, fluxo)
	if rec.Code != http.StatusOK {
		t.Fatalf("gravar fluxo: %d %s", rec.Code, rec.Body)
	}
	etapas := decodificar[struct {
		Etapas []etapaJSON `json:"etapas"`
	}](t, rec).Etapas
	if len(etapas) != 2 || etapas[0].Ordem != 1 || etapas[1].Ordem != 2 {
		t.Fatalf("a ordem devia sair 1 e 2, na ordem da lista: %+v", etapas)
	}
	if len(etapas[1].Campos) != 2 || etapas[1].Campos[0].ID != "nome" {
		t.Errorf("os campos deviam voltar como foram mandados: %+v", etapas[1].Campos)
	}

	// Regravar substitui o fluxo inteiro e renumera.
	rec = op.enviarJSON(http.MethodPut, rota, map[string]any{"etapas": []map[string]any{
		{"nome": "Só uma", "objetivo": "Descobrir o que a pessoa procura"},
	}})
	if etapas := decodificar[struct {
		Etapas []etapaJSON `json:"etapas"`
	}](t, rec).Etapas; len(etapas) != 1 || etapas[0].Ordem != 1 {
		t.Errorf("regravar devia deixar só a etapa nova, na ordem 1: %+v", etapas)
	}

	// Campo repetido é 400 com a etapa no texto.
	if rec := op.enviarJSON(http.MethodPut, rota, map[string]any{"etapas": []map[string]any{
		{"nome": "X", "objetivo": "Descobrir o que a pessoa procura", "campos": []map[string]any{
			{"id": "nome", "rotulo": "Nome"}, {"id": "nome", "rotulo": "De novo"}}},
	}}); rec.Code != http.StatusBadRequest {
		t.Errorf("campo repetido devia dar 400, veio %d", rec.Code)
	}

	// Com conversa aberta, mudar o fluxo é recusado: a pessoa ficaria numa etapa inexistente.
	s.exec(t, "INSERT INTO atendimento.conversas (bot_id, telefone) VALUES ($1, '5511900000000')", bot.ID)
	if rec := op.enviarJSON(http.MethodPut, rota, fluxo); rec.Code != http.StatusConflict {
		t.Errorf("fluxo com conversa aberta devia dar 409, veio %d %s", rec.Code, rec.Body)
	}
	// E o bot com conversa não se exclui: é histórico.
	if rec := op.enviarJSON(http.MethodDelete, "/atendimento/bots/"+idTexto(bot.ID), nil); rec.Code != http.StatusConflict {
		t.Errorf("bot com conversa devia dar 409 ao excluir, veio %d", rec.Code)
	}
	// Encerrada a conversa, o fluxo volta a ser editável.
	s.exec(t, "UPDATE atendimento.conversas SET status = 'encerrada' WHERE bot_id = $1", bot.ID)
	if rec := op.enviarJSON(http.MethodPut, rota, fluxo); rec.Code != http.StatusOK {
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
