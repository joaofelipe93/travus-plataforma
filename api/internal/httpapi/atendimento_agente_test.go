package httpapi

// Etapa B da issue #100: o agente do bot. **Nenhum teste chama a Anthropic** nem fala com o
// WhatsApp — o modelo é um dublê com roteiro, e a "resposta enviada" é uma linha na fila.
//
// O que está guardado aqui é o que precisa valer independente do que o modelo faça: a resposta
// não sai em ensaio, a etapa só avança com os obrigatórios coletados, o teto de tokens encerra a
// conversa, o teto da hora cancela a resposta, e a conversa do mesmo telefone continua a mesma.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// turnoBot é uma rodada do dublê: ou ele chama uma ferramenta, ou escreve a resposta final.
type turnoBot struct {
	ferramenta string
	entrada    string
	texto      string
}

// modeloRoteiroBot segue o roteiro na ordem e guarda o que recebeu, para o teste conferir o
// prompt e os resultados que as ferramentas devolveram.
type modeloRoteiroBot struct {
	mu         sync.Mutex
	turnos     []turnoBot
	i          int
	sistema    []string
	resultados []string
}

func (m *modeloRoteiroBot) Nome() string { return "duble-bot" }

func (m *modeloRoteiroBot) Gerar(_ context.Context, p assistente.Pedido, aoTexto func(string)) (*anthropic.BetaMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range p.Sistema {
		m.sistema = append(m.sistema, b.Text)
	}
	if ultima := p.Mensagens[len(p.Mensagens)-1]; len(ultima.Content) > 0 {
		for _, c := range ultima.Content {
			if res := c.OfToolResult; res != nil && len(res.Content) > 0 && res.Content[0].OfText != nil {
				m.resultados = append(m.resultados, res.Content[0].OfText.Text)
			}
		}
	}
	var t turnoBot
	if m.i < len(m.turnos) {
		t = m.turnos[m.i]
		m.i++
	} else {
		t = turnoBot{texto: "Tudo certo!"}
	}
	var bruto string
	if t.ferramenta != "" {
		entrada := t.entrada
		if entrada == "" {
			entrada = "{}"
		}
		bruto = `{"id":"m","type":"message","role":"assistant","model":"duble","stop_reason":"tool_use",
			"content":[{"type":"tool_use","id":"toolu_1","name":"` + t.ferramenta + `","input":` + entrada + `}],
			"usage":{"input_tokens":5,"output_tokens":3}}`
	} else {
		aoTexto(t.texto)
		bruto = `{"id":"m","type":"message","role":"assistant","model":"duble","stop_reason":"end_turn",
			"content":[{"type":"text","text":` + jsonTexto(t.texto) + `}],"usage":{"input_tokens":5,"output_tokens":3}}`
	}
	var msg anthropic.BetaMessage
	if err := json.Unmarshal([]byte(bruto), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func jsonTexto(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// botComPerguntas cria um bot com duas perguntas e devolve o bot pronto para conversar.
// `ligado` é o único interruptor que existe (29/10/2026). Os exemplos são genéricos: a Travus
// trabalha com vários segmentos.
func botComPerguntas(t *testing.T, s *Servidor, slug string, ligado bool) db.AtendimentoBot {
	t.Helper()
	ctx := context.Background()
	bot, err := s.q.CriarBot(ctx, db.CriarBotParams{
		Nome: "Bot " + slug, Slug: slug, Modelo: "claude-haiku-5-5",
		Instrucoes:       "Você é o primeiro atendimento da Travus e fala só do que está aqui.",
		MaxMensagensHora: 60, MaxTokensConversa: 50000, PausaRespostaSegundos: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range []struct {
		texto, campo string
	}{
		{"Qual é o nome da empresa?", "nome_da_empresa"},
		{"Qual o melhor telefone para a equipe ligar?", "melhor_telefone"},
	} {
		if _, err := s.q.CriarPergunta(ctx, db.CriarPerguntaParams{
			BotID: bot.ID, Ordem: int32(i + 1), Texto: q.texto, Campo: q.campo, Obrigatoria: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	bot, err = s.q.AtualizarBot(ctx, db.AtualizarBotParams{
		ID: bot.ID, Nome: bot.Nome, Modelo: bot.Modelo, Instrucoes: bot.Instrucoes, Ativo: ligado,
		MaxMensagensHora: 60, MaxTokensConversa: 50000, PausaRespostaSegundos: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

// servidorComBot monta o servidor com o dublê já ligado.
func servidorComBot(t *testing.T, m *modeloRoteiroBot) *Servidor {
	t.Helper()
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	s.cfg.ModeloAtendimento = func(string) assistente.Modelo { return m }
	return s
}

func TestAtendimentoRespondeEColeta(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"campo":"nome_da_empresa","valor":"Acme Ltda"}`},
		{texto: "Perfeito! Em qual bairro?"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "imoveis", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511900000000", "Fulano", "quero alugar um ap de 2 quartos")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "pendente" || res.MensagemID == nil {
		t.Fatalf("com as duas chaves ligadas a resposta devia entrar na fila: %+v", res)
	}
	if res.Encerrada {
		t.Error("a conversa não devia encerrar")
	}

	// O dado foi gravado, e é isso que a etapa E vai virar lead.
	coletas, err := s.q.ColetasDaConversa(context.Background(), res.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(coletas) != 1 || coletas[0].Campo != "nome_da_empresa" {
		t.Fatalf("a coleta devia estar gravada: %+v", coletas)
	}
	// O prompt precisa dizer quem o bot é, as regras da casa e a etapa atual.
	prompt := strings.Join(m.sistema, "\n")
	for _, esperado := range []string{
		"Travus", "atendimento automático",
		"1. Qual é o nome da empresa?",
		"Agora trate da pergunta 1", // o prompt é montado antes de a ferramenta rodar
	} {
		if !strings.Contains(prompt, esperado) {
			t.Errorf("o prompt devia conter %q", esperado)
		}
	}
	// A ferramenta devolve ao modelo qual é a próxima pergunta.
	if !strings.Contains(strings.Join(m.resultados, " "), "proxima_pergunta") {
		t.Errorf("registrar_resposta devia dizer a próxima pergunta: %q", m.resultados)
	}
}

// A próxima pergunta é a primeira sem resposta: não existe "avançar". Registrar a resposta é o
// que faz a lista andar — e é a plataforma que calcula, não o modelo.
func TestAtendimentoListaAndaComAResposta(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"campo":"nome_da_empresa","valor":"Acme Ltda"}`},
		{texto: "Obrigado! Qual o melhor telefone?"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "andando", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511922222222", "", "somos a Acme")
	if err != nil {
		t.Fatal(err)
	}
	// Respondida a primeira, a pergunta da vez passa a ser a segunda.
	if res.PerguntaAtual == nil || *res.PerguntaAtual != "Qual o melhor telefone para a equipe ligar?" {
		t.Fatalf("a próxima pergunta devia ser a segunda: %+v", res.PerguntaAtual)
	}
	if res.Encerrada {
		t.Error("ainda falta uma pergunta: não devia encerrar")
	}
	// A ferramenta devolve ao modelo qual é a próxima — é assim que ele sabe o que perguntar.
	if !strings.Contains(strings.Join(m.resultados, " "), "melhor telefone") {
		t.Errorf("a ferramenta devia dizer a próxima pergunta: %q", m.resultados)
	}
}

// Dado que não é de pergunta nenhuma é recusado: senão o modelo inventa chave e a tela mostra
// dado que ninguém pediu. A recusa volta para ele, que corrige sozinho.
func TestAtendimentoRecusaDadoDesconhecido(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"campo":"cor_favorita","valor":"azul"}`},
		{texto: "Qual é o nome da empresa?"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "inventado", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511922222223", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if res.PerguntaAtual == nil || *res.PerguntaAtual != "Qual é o nome da empresa?" {
		t.Errorf("nada foi registrado: a pergunta da vez continua a primeira: %+v", res.PerguntaAtual)
	}
	juntos := strings.Join(m.resultados, " ")
	if !strings.Contains(juntos, "nome_da_empresa") || !strings.Contains(juntos, "melhor_telefone") {
		t.Errorf("a recusa devia listar os dados que existem: %q", m.resultados)
	}
	var quantas int
	if err := s.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM atendimento.coletas WHERE conversa_id = $1", res.ConversaID).Scan(&quantas); err != nil {
		t.Fatal(err)
	}
	if quantas != 0 {
		t.Errorf("o dado inventado não podia ser gravado: %d coleta(s)", quantas)
	}
}

// Respondidas todas, o bot encerra — e a última mensagem ainda sai.
func TestAtendimentoEncerraQuandoRespondeTudo(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"campo":"nome_da_empresa","valor":"Acme Ltda"}`},
		{ferramenta: "registrar_resposta", entrada: `{"campo":"melhor_telefone","valor":"11988887777"}`},
		{ferramenta: "encerrar", entrada: `{"motivo":"concluido"}`},
		{texto: "Obrigado! A equipe vai falar com você."},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "completo", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511933333333",
		"", "somos a Acme Ltda, meu telefone é 11988887777")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encerrada || res.Motivo != "concluido" {
		t.Fatalf("devia encerrar como concluído: %+v", res)
	}
	if res.Status != "pendente" || res.MensagemID == nil {
		t.Errorf("a última resposta devia entrar na fila: %+v", res)
	}
	if res.PerguntaAtual != nil {
		t.Errorf("não há próxima pergunta: %+v", res.PerguntaAtual)
	}
	respostas, _ := s.q.ColetasDaConversa(context.Background(), res.ConversaID)
	if len(respostas) != 2 {
		t.Errorf("as duas respostas deviam estar gravadas: %+v", respostas)
	}
}

func TestAtendimentoEncerraPedidoDeHumano(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "encerrar", entrada: `{"motivo":"pediu_humano"}`},
		{texto: "Vou passar para a equipe, já te respondem por aqui."},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "humano", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511944444444", "", "quero falar com uma pessoa")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encerrada || res.Status != "pendente" {
		t.Fatalf("devia encerrar e ainda assim avisar a pessoa: %+v", res)
	}
	var motivo string
	if err := s.pool.QueryRow(context.Background(),
		"SELECT motivo FROM atendimento.conversas WHERE id = $1", res.ConversaID).Scan(&motivo); err != nil {
		t.Fatal(err)
	}
	if motivo != "pediu_humano" {
		t.Errorf("o motivo devia ficar gravado para a tela agrupar: %q", motivo)
	}
}

func TestAtendimentoMotivoDesconhecidoNaoEncerra(t *testing.T) {
	// Motivo inventado é recusado: a tela agrupa por esses valores.
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "encerrar", entrada: `{"motivo":"cansei"}`},
		{texto: "Posso ajudar em mais algo?"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "motivo", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511955555555", "", "tchau")
	if err != nil {
		t.Fatal(err)
	}
	if res.Encerrada {
		t.Error("motivo desconhecido não devia encerrar a conversa")
	}
	if !strings.Contains(strings.Join(m.resultados, " "), "pediu_humano") {
		t.Errorf("o erro devia listar os motivos válidos: %q", m.resultados)
	}
}

func TestAtendimentoTetoDeTokensEncerra(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "teto-tokens", true)
	// Teto baixo e conversa já gasta: a próxima mensagem encerra em vez de chamar o modelo.
	if _, err := s.q.AtualizarBot(context.Background(), db.AtualizarBotParams{
		ID: bot.ID, Nome: bot.Nome, Modelo: bot.Modelo, Instrucoes: bot.Instrucoes, Ativo: true,
		MaxMensagensHora: 60, MaxTokensConversa: 1000, PausaRespostaSegundos: 0,
	}); err != nil {
		t.Fatal(err)
	}
	bot, _ = s.q.BuscarBot(context.Background(), bot.ID)

	res, err := s.responderAtendimento(context.Background(), bot, "5511966666666", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	s.exec(t, "UPDATE atendimento.conversas SET tokens_entrada = 2000 WHERE id = $1", res.ConversaID)

	res, err = s.responderAtendimento(context.Background(), bot, "5511966666666", "", "e aí?")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encerrada || res.Status != "sem_resposta" || !strings.Contains(res.Motivo, "teto de tokens") {
		t.Fatalf("passado o teto, a conversa devia encerrar sem resposta: %+v", res)
	}
}

func TestAtendimentoTetoDaHoraCancela(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}, {texto: "Oi de novo!"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "teto-hora", true)
	s.exec(t, "UPDATE atendimento.bots SET max_mensagens_hora = 1 WHERE id = $1", bot.ID)
	bot, _ = s.q.BuscarBot(context.Background(), bot.ID)

	primeira, err := s.responderAtendimento(context.Background(), bot, "5511977777777", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if primeira.Status != "pendente" {
		t.Fatalf("a primeira devia sair: %+v", primeira)
	}
	segunda, err := s.responderAtendimento(context.Background(), bot, "5511988888888", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if segunda.Status != "cancelada" || !strings.Contains(segunda.Motivo, "teto") {
		t.Fatalf("passado o teto da hora, a resposta devia nascer cancelada: %+v", segunda)
	}
}

func TestAtendimentoMesmoTelefoneContinuaAConversa(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}, {texto: "Pois não?"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "mesma-conversa", true)

	a, err := s.responderAtendimento(context.Background(), bot, "5511999999999", "Fulano", "oi")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.responderAtendimento(context.Background(), bot, "5511999999999", "", "ainda estou aqui")
	if err != nil {
		t.Fatal(err)
	}
	if a.ConversaID != b.ConversaID {
		t.Fatalf("a segunda mensagem devia continuar a mesma conversa: %d e %d", a.ConversaID, b.ConversaID)
	}
	// O nome do contato não se perde quando a segunda mensagem vem sem ele.
	var nome *string
	if err := s.pool.QueryRow(context.Background(),
		"SELECT contato_nome FROM atendimento.conversas WHERE id = $1", a.ConversaID).Scan(&nome); err != nil {
		t.Fatal(err)
	}
	if nome == nil || *nome != "Fulano" {
		t.Errorf("o nome do contato devia continuar gravado: %v", nome)
	}
}

func TestAtendimentoBotDesligadoNaoAbreConversa(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "não devia chegar aqui"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "desligado", true)
	s.exec(t, "UPDATE atendimento.bots SET ativo = false WHERE id = $1", bot.ID)
	bot, _ = s.q.BuscarBot(context.Background(), bot.ID)

	res, err := s.responderAtendimento(context.Background(), bot, "5511900000001", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "sem_resposta" || res.ConversaID != 0 {
		t.Fatalf("bot desligado não devia nem abrir conversa: %+v", res)
	}
	if m.i != 0 {
		t.Error("o modelo não devia ser chamado com o bot desligado")
	}
}

func TestAtendimentoSemChaveNoServidor(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	s.cfg.ModeloAtendimento = nil // sem ANTHROPIC_API_KEY
	bot := botComPerguntas(t, s, "sem-chave", true)

	if _, err := s.responderAtendimento(context.Background(), bot, "5511900000002", "", "oi"); err == nil {
		t.Fatal("sem chave, responder devia falhar com erro claro")
	} else if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("o erro devia dizer o que falta: %v", err)
	}
}

func TestAtendimentoRotaInternaExigeToken(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Olá!"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "interno", true)
	interno := s.RotasInternas()

	corpo := map[string]any{"bot": bot.Slug, "telefone": "5511900000003", "texto": "oi"}
	// Sem o token de serviço, nada.
	if rec := enviarInterno(t, interno, corpo, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token devia dar 401, veio %d", rec.Code)
	}
	rec := enviarInterno(t, interno, corpo, tokenWorkerTeste)
	if rec.Code != http.StatusOK {
		t.Fatalf("com token devia responder: %d %s", rec.Code, rec.Body)
	}
	res := decodificar[resultadoAtendimento](t, rec)
	if res.ConversaID == 0 || res.Status != "pendente" {
		t.Errorf("a rota devia abrir a conversa e enfileirar: %+v", res)
	}
	// Bot que não existe é 404, não erro de servidor.
	if rec := enviarInterno(t, interno, map[string]any{"bot": "nao-existe", "telefone": "5511900000004", "texto": "oi"}, tokenWorkerTeste); rec.Code != http.StatusNotFound {
		t.Errorf("bot inexistente devia dar 404, veio %d", rec.Code)
	}
	// Telefone com máscara é normalizado para só dígitos (o CHECK da tabela exige).
	if rec := enviarInterno(t, interno, map[string]any{"bot": bot.Slug, "telefone": "+55 (11) 90000-0005", "texto": "oi"}, tokenWorkerTeste); rec.Code != http.StatusOK {
		t.Errorf("telefone com máscara devia ser aceito, veio %d %s", rec.Code, rec.Body)
	}
}

// enviarInterno fala com a rota interna como o worker: token de serviço no Authorization.
func enviarInterno(t *testing.T, h http.Handler, corpo any, token string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(corpo)
	r := httptest.NewRequest(http.MethodPost, "/internal/atendimento/mensagens", bytes.NewReader(b))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	// As rotas internas exigem saber qual worker falou: é o que aparece no log da conversa.
	r.Header.Set("X-Worker", "worker-de-teste")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}
