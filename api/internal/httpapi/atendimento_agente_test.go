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

// botComFluxo cria um bot ativo com duas etapas e devolve o bot pronto para conversar.
func botComFluxo(t *testing.T, s *Servidor, slug string, envioDoBot bool) db.AtendimentoBot {
	t.Helper()
	ctx := context.Background()
	bot, err := s.q.CriarBot(ctx, db.CriarBotParams{
		Nome: "Bot " + slug, Slug: slug, Modelo: "claude-haiku-5-5",
		Instrucoes:       "Você atende a imobiliária e só fala de aluguel.",
		MaxMensagensHora: 60, MaxTokensConversa: 50000, PausaRespostaSegundos: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range []struct {
		nome, objetivo, campos string
	}{
		{"Interesse", "Descobrir o que a pessoa procura", `[{"id":"interesse","rotulo":"Interesse","obrigatorio":true}]`},
		{"Contato", "Pegar o telefone para o corretor ligar", `[{"id":"telefone","rotulo":"Telefone","obrigatorio":true}]`},
	} {
		if _, err := s.q.CriarEtapa(ctx, db.CriarEtapaParams{
			BotID: bot.ID, Ordem: int32(i + 1), Nome: e.nome, Objetivo: e.objetivo, Campos: []byte(e.campos),
		}); err != nil {
			t.Fatal(err)
		}
	}
	bot, err = s.q.AtualizarBot(ctx, db.AtualizarBotParams{
		ID: bot.ID, Nome: bot.Nome, Modelo: bot.Modelo, Instrucoes: bot.Instrucoes, Ativo: true,
		MaxMensagensHora: 60, MaxTokensConversa: 50000, PausaRespostaSegundos: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if envioDoBot {
		if bot, err = s.q.DefinirEnvioDoBot(ctx, db.DefinirEnvioDoBotParams{ID: bot.ID, EnvioHabilitado: true}); err != nil {
			t.Fatal(err)
		}
	}
	return bot
}

// servidorComBot monta o servidor com o dublê já ligado.
func servidorComBot(t *testing.T, m *modeloRoteiroBot, envioDoServidor bool) *Servidor {
	t.Helper()
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	s.cfg.ModeloAtendimento = func(string) assistente.Modelo { return m }
	s.cfg.AtendimentoEnvioHabilitado = envioDoServidor
	return s
}

func TestAtendimentoRespondeEColeta(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_dado", entrada: `{"campo":"interesse","valor":"apartamento 2 quartos"}`},
		{texto: "Perfeito! Em qual bairro?"},
	}}
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "imoveis", true)

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
	if len(coletas) != 1 || coletas[0].Campo != "interesse" {
		t.Fatalf("a coleta devia estar gravada: %+v", coletas)
	}
	// O prompt precisa dizer quem o bot é, as regras da casa e a etapa atual.
	prompt := strings.Join(m.sistema, "\n")
	for _, esperado := range []string{"imobiliária", "atendimento automático", "Etapa atual: Interesse", "interesse (Interesse, obrigatório)"} {
		if !strings.Contains(prompt, esperado) {
			t.Errorf("o prompt devia conter %q", esperado)
		}
	}
	// Com o obrigatório coletado, a ferramenta avisa o modelo que a etapa fechou.
	if !strings.Contains(strings.Join(m.resultados, " "), "etapa_completa") {
		t.Errorf("registrar_dado devia dizer que a etapa fechou: %q", m.resultados)
	}
}

func TestAtendimentoEnsaioNaoManda(t *testing.T) {
	casos := map[string]struct {
		servidor, bot bool
		motivo        string
	}{
		"chave do servidor desligada": {servidor: false, bot: true, motivo: "ATENDIMENTO_ENVIO_HABILITADO"},
		"envio do bot desligado":      {servidor: true, bot: false, motivo: "envio deste bot"},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Como posso ajudar?"}}}
			s := servidorComBot(t, m, c.servidor)
			bot := botComFluxo(t, s, "ensaio", c.bot)

			res, err := s.responderAtendimento(context.Background(), bot, "5511911111111", "", "oi")
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != "cancelada" || !strings.Contains(res.Motivo, c.motivo) {
				t.Fatalf("devia nascer cancelada com o motivo do ensaio: %+v", res)
			}
			// A resposta fica gravada: é assim que a tela mostra o que teria saído.
			msgs, err := s.q.HistoricoDaConversa(context.Background(), db.HistoricoDaConversaParams{
				ConversaID: res.ConversaID, Limite: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, msg := range msgs {
				if msg.Direcao == "enviada" {
					t.Error("mensagem cancelada não devia aparecer no histórico do modelo")
				}
			}
			var texto string
			if err := s.pool.QueryRow(context.Background(),
				"SELECT texto FROM atendimento.mensagens WHERE conversa_id = $1 AND direcao = 'enviada'",
				res.ConversaID).Scan(&texto); err != nil {
				t.Fatalf("a resposta devia estar gravada mesmo cancelada: %v", err)
			}
			if texto == "" {
				t.Error("o texto do que sairia precisa ficar guardado")
			}
		})
	}
}

func TestAtendimentoEtapaSoAvancaComObrigatorio(t *testing.T) {
	// O modelo tenta correr: pede para avançar antes de coletar o obrigatório.
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "avancar_etapa"},
		{texto: "Me conta o que você procura?"},
	}}
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "apressado", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511922222222", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if res.EtapaAtual == nil || *res.EtapaAtual != "Interesse" {
		t.Fatalf("a conversa devia ficar na primeira etapa: %+v", res)
	}
	// A recusa volta para o modelo dizendo o que falta — é assim que ele corrige sozinho.
	if !strings.Contains(strings.Join(m.resultados, " "), "interesse") {
		t.Errorf("a ferramenta devia dizer qual dado falta: %q", m.resultados)
	}
}

func TestAtendimentoAvancaEEncerraNoFim(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_dado", entrada: `{"campo":"interesse","valor":"kitnet"}`},
		{ferramenta: "avancar_etapa"},
		{ferramenta: "registrar_dado", entrada: `{"campo":"telefone","valor":"11988887777"}`},
		{ferramenta: "avancar_etapa"}, // última etapa: avançar é terminar
		{texto: "Obrigado! Um corretor vai te ligar."},
	}}
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "completo", true)

	res, err := s.responderAtendimento(context.Background(), bot, "5511933333333", "", "quero uma kitnet, meu zap é 11988887777")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encerrada || res.Motivo != "concluido" {
		t.Fatalf("o fluxo acabou: devia encerrar como concluído: %+v", res)
	}
	// A última mensagem ainda sai: encerrar não pode cancelar a despedida.
	if res.Status != "pendente" || res.MensagemID == nil {
		t.Errorf("a última resposta devia entrar na fila: %+v", res)
	}
	coletas, _ := s.q.ColetasDaConversa(context.Background(), res.ConversaID)
	if len(coletas) != 2 {
		t.Errorf("os dois dados deviam estar gravados: %+v", coletas)
	}
}

func TestAtendimentoEncerraPedidoDeHumano(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "encerrar", entrada: `{"motivo":"pediu_humano"}`},
		{texto: "Vou passar para a equipe, já te respondem por aqui."},
	}}
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "humano", true)

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
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "motivo", true)

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
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "teto-tokens", true)
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
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "teto-hora", true)
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
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "mesma-conversa", true)

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
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "desligado", true)
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
	bot := botComFluxo(t, s, "sem-chave", true)

	if _, err := s.responderAtendimento(context.Background(), bot, "5511900000002", "", "oi"); err == nil {
		t.Fatal("sem chave, responder devia falhar com erro claro")
	} else if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("o erro devia dizer o que falta: %v", err)
	}
}

func TestAtendimentoRotaInternaExigeToken(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Olá!"}}}
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "interno", true)
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
