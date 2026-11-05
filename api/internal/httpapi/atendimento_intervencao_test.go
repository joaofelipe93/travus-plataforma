package httpapi

// Issue #128, etapa 1: o bot não atropela quem já está sendo atendido, e quem volta a escrever
// continua na mesma conversa.
//
// O que estes testes guardam é o que custou caro em produção em 05/11/2026: alguém pediu
// atendimento humano, escreveu de novo quatro minutos depois e o bot começou do zero,
// reperguntando o nome da empresa.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// mensagemDoWorker entrega uma mensagem pela rota interna, como o worker faz.
func mensagemDoWorker(t *testing.T, s *Servidor, corpo map[string]any) map[string]any {
	t.Helper()
	rec := avisarWorker(t, s, "/internal/atendimento/mensagens", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("entregar mensagem: %d %s", rec.Code, rec.Body)
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

// A pessoa que volta a escrever continua na MESMA conversa, com o que já contou.
func TestAtendimentoQuemVoltaContinuaNaMesmaConversa(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		// Primeira passagem: colhe a empresa e encerra a pedido dela.
		{ferramenta: "registrar_resposta", entrada: `{"campo":"nome_da_empresa","valor":"Acme Ltda"}`},
		{texto: "Combinado, um consultor responde por aqui.",
			ferramenta: "encerrar", entrada: `{"motivo":"pediu_humano"}`},
		// Segunda passagem, dias depois.
		{texto: "Oi de novo! Já tenho a Acme aqui."},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "volta", true)
	ctx := context.Background()

	primeira, err := s.responderAtendimento(ctx, bot, "5511933333333", "Fulano", "somos a Acme, quero falar com alguém", "")
	if err != nil {
		t.Fatal(err)
	}
	if !primeira.Encerrada {
		t.Fatalf("a primeira passagem devia encerrar: %+v", primeira)
	}

	segunda, err := s.responderAtendimento(ctx, bot, "5511933333333", "Fulano", "voltei", "")
	if err != nil {
		t.Fatal(err)
	}
	if segunda.ConversaID != primeira.ConversaID {
		t.Fatalf("devia continuar na mesma conversa: %d e %d", primeira.ConversaID, segunda.ConversaID)
	}

	// A conversa voltou a estar aberta, e o que ela já havia colhido continua lá.
	c, err := s.q.BuscarConversa(ctx, segunda.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "aberta" {
		t.Errorf("a conversa devia estar aberta de novo, está %q", c.Status)
	}
	coletas, err := s.q.ColetasDaConversa(ctx, segunda.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(coletas) != 1 || coletas[0].Valor != "Acme Ltda" {
		t.Fatalf("os dados da primeira passagem deviam continuar: %+v", coletas)
	}
	// E o bot foi avisado de que não precisa perguntar de novo (na instrução da vez, que é onde
	// mora o que muda — ver a divisão do prompt na issue #128).
	daVez := strings.Join(m.daVez, "\n")
	if !strings.Contains(daVez, "JÁ RESPONDIDA") || !strings.Contains(daVez, "Acme Ltda") {
		t.Errorf("a instrução da volta devia dizer o que já se sabe: %q", daVez)
	}
}

// Conversa reaberta não herda o gasto das passagens anteriores: o teto por conversa vale para o
// trecho atual, senão quem volta depois de uma conversa longa nunca mais é atendido.
func TestAtendimentoTetoDeTokensNaoHerdaOGastoAnterior(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{texto: "Oi!", ferramenta: "encerrar", entrada: `{"motivo":"concluido"}`},
		{texto: "Olá de novo!"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "teto-marco", true)
	ctx := context.Background()

	primeira, err := s.responderAtendimento(ctx, bot, "5511944444444", "", "oi", "")
	if err != nil {
		t.Fatal(err)
	}
	// Gasto de uma conversa inteira, bem acima do teto do bot.
	if _, err := s.q.SomarTokensDaConversa(ctx, db.SomarTokensDaConversaParams{
		ID: primeira.ConversaID, TokensEntrada: 49_000, TokensSaida: 2_000,
	}); err != nil {
		t.Fatal(err)
	}

	segunda, err := s.responderAtendimento(ctx, bot, "5511944444444", "", "voltei", "")
	if err != nil {
		t.Fatal(err)
	}
	if segunda.Encerrada {
		t.Fatalf("a volta não devia bater no teto: %+v", segunda)
	}
	if segunda.Status != "pendente" {
		t.Errorf("a resposta devia entrar na fila: %+v", segunda)
	}
}

// Consultor responde pelo celular do bot: a conversa passa a ser dele e o bot se cala.
func TestAtendimentoPessoaAssumePeloWhatsapp(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Qual é o nome da empresa?"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "assume-wa", true)
	ctx := context.Background()

	primeira, err := s.responderAtendimento(ctx, bot, "5511955555555", "Fulano", "oi", "")
	if err != nil {
		t.Fatal(err)
	}
	if primeira.Status != "pendente" {
		t.Fatalf("o bot devia ter respondido: %+v", primeira)
	}

	// O consultor escreve pelo celular: mensagem que SAIU do número do bot, com um id que não é
	// de nenhuma resposta da plataforma.
	res := mensagemDoWorker(t, s, map[string]any{
		"bot": bot.Slug, "telefone": "5511955555555", "texto": "Oi, aqui é o Rodrigo, eu assumo daqui",
		"do_numero_do_bot": true, "wa_id": "3EB0DOCONSULTOR",
	})
	if res["conversa_id"] == nil {
		t.Fatalf("devia ter achado a conversa: %+v", res)
	}

	// A resposta que estava na fila não sai por cima de quem assumiu.
	msgs, err := s.q.MensagensDaConversa(ctx, primeira.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	var pendentes, dePessoa int
	for _, x := range msgs {
		if x.Status == "pendente" {
			pendentes++
		}
		if x.DePessoa {
			dePessoa++
		}
	}
	if pendentes != 0 {
		t.Errorf("a resposta pendente devia ter sido cancelada: %+v", msgs)
	}
	if dePessoa != 1 {
		t.Errorf("a mensagem do consultor devia estar registrada como de pessoa: %+v", msgs)
	}

	// E agora o bot não responde mais nesta conversa.
	antes := len(m.sistema)
	depois, err := s.responderAtendimento(ctx, bot, "5511955555555", "Fulano", "obrigado!", "")
	if err != nil {
		t.Fatal(err)
	}
	if depois.Status != "sem_resposta" || depois.Motivo != motivoAssumida {
		t.Fatalf("com a conversa assumida o bot devia ficar quieto: %+v", depois)
	}
	if len(m.sistema) != antes {
		t.Error("o modelo não devia nem ser chamado")
	}
	// A mensagem da pessoa continua registrada: quem atende precisa lê-la.
	msgs, err = s.q.MensagensDaConversa(ctx, primeira.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[len(msgs)-1].Texto != "obrigado!" {
		t.Errorf("a última mensagem recebida devia estar gravada: %+v", msgs[len(msgs)-1])
	}
}

// O eco da própria resposta volta como `fromMe` e NÃO pode ser lido como intervenção humana:
// senão todo bot se daria por assumido na primeira resposta que mandasse.
func TestAtendimentoEcoDaPropriaRespostaNaoAssume(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Qual é o nome da empresa?"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "eco", true)
	ctx := context.Background()

	res, err := s.responderAtendimento(ctx, bot, "5511966666666", "", "oi", "")
	if err != nil {
		t.Fatal(err)
	}
	// O worker pega a fila e registra o envio com o id do WhatsApp.
	fila := filaDoWorker(t, s, []string{bot.Slug})
	if len(fila) != 1 {
		t.Fatalf("a fila devia ter a resposta: %+v", fila)
	}
	if rec := avisarWorker(t, s, "/internal/atendimento/mensagens/"+idTexto(fila[0].ID)+"/enviada",
		map[string]any{"wa_id": "3EB0NOSSAMENSAGEM"}); rec.Code != http.StatusOK {
		t.Fatalf("registrar envio: %d %s", rec.Code, rec.Body)
	}

	// Agora o WhatsApp devolve essa mesma mensagem como `fromMe`.
	eco := mensagemDoWorker(t, s, map[string]any{
		"bot": bot.Slug, "telefone": "5511966666666", "texto": "Oi! Qual é o nome da empresa?",
		"do_numero_do_bot": true, "wa_id": "3EB0NOSSAMENSAGEM",
	})
	if eco["motivo"] != "eco da própria resposta" {
		t.Fatalf("o eco devia ser reconhecido: %+v", eco)
	}
	c, err := s.q.BuscarConversa(ctx, res.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if c.AssumidaEm != nil {
		t.Error("a conversa não devia ter sido dada como assumida")
	}
}

// Conversa endereçada por LID: a mensagem que sai pelo bot não traz o telefone de quem está do
// outro lado, e é pelo LID guardado na conversa que ela é reconhecida.
func TestAtendimentoPessoaAssumeConversaDeLid(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Qual é o nome da empresa?"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "assume-lid", true)
	ctx := context.Background()

	// A pessoa escreve: o telefone vem do `sender_pn` e o LID é o endereço da conversa.
	primeira, err := s.responderAtendimento(ctx, bot, "5511977777777", "Fulano", "oi", "213090591355025")
	if err != nil {
		t.Fatal(err)
	}

	// O consultor responde pelo celular: só o LID vem.
	res := mensagemDoWorker(t, s, map[string]any{
		"bot": bot.Slug, "texto": "eu assumo", "lid": "213090591355025",
		"do_numero_do_bot": true, "wa_id": "3EB0PELOLID",
	})
	if int64(res["conversa_id"].(float64)) != primeira.ConversaID {
		t.Fatalf("devia achar a conversa pelo LID: %+v", res)
	}
	c, err := s.q.BuscarConversa(ctx, primeira.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if c.AssumidaEm == nil {
		t.Error("a conversa devia ter sido assumida")
	}
}

// Texto escrito depois de `encerrar` não vai para o WhatsApp: a pessoa leria a despedida duas
// vezes (a "quebra da mensagem" da issue #128).
func TestAtendimentoNaoMandaTextoDepoisDeEncerrar(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		// A despedida e o encerrar vêm juntos, como o modelo faz…
		{texto: "Entendi. Vou passar seu pedido para a equipe.",
			ferramenta: "encerrar", entrada: `{"motivo":"pediu_humano"}`},
		// …e depois ele escreve de novo, agora que a ferramenta respondeu. Este segundo texto é
		// o que não pode chegar à pessoa.
		{texto: "Pronto, o atendimento foi encerrado e um consultor vai responder."},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "quebra", true)
	ctx := context.Background()

	res, err := s.responderAtendimento(ctx, bot, "5511988888888", "", "quero falar com uma pessoa", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encerrada {
		t.Fatalf("devia encerrar: %+v", res)
	}
	msgs, err := s.q.MensagensDaConversa(ctx, res.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	var enviada string
	for _, x := range msgs {
		if x.Direcao == "enviada" {
			enviada = x.Texto
		}
	}
	if strings.Contains(enviada, "foi encerrado") {
		t.Errorf("o texto de depois do encerrar não devia sair: %q", enviada)
	}
	if !strings.Contains(enviada, "passar seu pedido") {
		t.Errorf("a despedida de antes do encerrar devia sair: %q", enviada)
	}
}

// Issue #128, item "tokens gastos, otimizar". O que estes testes guardam não é um número de
// tokens (isso depende do modelo), e sim a forma do pedido que torna o cache possível: o que muda
// a cada mensagem não pode estar no bloco cacheado.

func TestAtendimentoMandaOQueMudaNoFimDasMensagens(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"campo":"nome_da_empresa","valor":"Acme Ltda"}`},
		{texto: "Obrigado! Qual o melhor telefone?"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "cache-forma", true)

	if _, err := s.responderAtendimento(context.Background(), bot, "5511900000001", "", "somos a Acme", ""); err != nil {
		t.Fatal(err)
	}

	cima := strings.Join(m.sistema, "\n")
	fim := strings.Join(m.daVez, "\n")
	// O bloco de cima tem quem o bot é e as perguntas…
	for _, esperado := range []string{"Travus", "1. Qual é o nome da empresa?", "Nunca peça senha"} {
		if !strings.Contains(cima, esperado) {
			t.Errorf("o bloco cacheado devia conter %q", esperado)
		}
	}
	// …e não tem nada que mude durante a conversa.
	for _, proibido := range []string{"JÁ RESPONDIDA", "Agora trate da pergunta"} {
		if strings.Contains(cima, proibido) {
			t.Errorf("%q no bloco cacheado invalida o cache a cada resposta:\n%s", proibido, cima)
		}
	}
	// A pergunta da vez chega como instrução de sistema no fim das mensagens.
	if !strings.Contains(fim, "Agora trate da pergunta") {
		t.Errorf("a instrução da vez devia ir no fim das mensagens:\n%s", fim)
	}
}

func TestAtendimentoGuardaOQueVeioDoCache(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Qual é o nome da empresa?"}}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "cache-conta", true)
	ctx := context.Background()

	res, err := s.responderAtendimento(ctx, bot, "5511900000002", "", "oi", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.q.BuscarConversa(ctx, res.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	// Sem isto gravado, "o cache está funcionando?" não tem resposta — e cache que morre é
	// silencioso: aparece só como conta maior no fim do mês.
	if c.TokensCacheLidos == 0 || c.TokensCacheGravados == 0 {
		t.Errorf("o cache devia estar contabilizado na conversa: lidos=%d gravados=%d",
			c.TokensCacheLidos, c.TokensCacheGravados)
	}
	// E o teto de tokens continua olhando só o que foi cobrado como entrada normal.
	if res.TokensGasto != c.TokensEntrada+c.TokensSaida {
		t.Errorf("o gasto do teto não devia incluir cache: %d vs %d", res.TokensGasto, c.TokensEntrada+c.TokensSaida)
	}
}

// Vários dados numa chamada só: cada rodada de ferramenta reenvia o prompt e o histórico
// inteiros, então registrar três coisas em três chamadas custa três pedidos ao modelo.
func TestAtendimentoRegistraVariosDadosNumaChamada(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"respostas":[
			{"campo":"nome_da_empresa","valor":"Acme Ltda"},
			{"campo":"melhor_telefone","valor":"11988887777"}]}`},
		{texto: "Perfeito, já tenho tudo.", ferramenta: "encerrar", entrada: `{"motivo":"concluido"}`},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "lote", true)
	ctx := context.Background()

	res, err := s.responderAtendimento(ctx, bot, "5511900000003", "", "somos a Acme, telefone 11988887777", "")
	if err != nil {
		t.Fatal(err)
	}
	coletas, err := s.q.ColetasDaConversa(ctx, res.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(coletas) != 2 {
		t.Fatalf("as duas respostas deviam ter sido gravadas numa chamada: %+v", coletas)
	}
	// Duas chamadas ao modelo: a que registra os dois dados e a que se despede encerrando. Com
	// uma chamada por dado seriam três, e sem o `Encerra` da ferramenta haveria uma quarta só
	// para o modelo escrever um texto que a plataforma descarta.
	if m.chamadas != 2 {
		t.Errorf("devia ter gastado 2 chamadas ao modelo, gastou %d", m.chamadas)
	}
	if !res.Encerrada {
		t.Error("com tudo respondido, devia encerrar")
	}
}

// Dado inventado continua sendo recusado, inclusive dentro de uma lista — senão a tela mostraria
// campo que ninguém configurou.
func TestAtendimentoRecusaDadoDesconhecidoNaLista(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"respostas":[
			{"campo":"nome_da_empresa","valor":"Acme"},
			{"campo":"cor_favorita","valor":"azul"}]}`},
		{texto: "Desculpe, vamos de novo: qual o melhor telefone?"},
	}}
	s := servidorComBot(t, m)
	bot := botComPerguntas(t, s, "lote-recusa", true)
	ctx := context.Background()

	res, err := s.responderAtendimento(ctx, bot, "5511900000004", "", "somos a Acme e gosto de azul", "")
	if err != nil {
		t.Fatal(err)
	}
	erro := strings.Join(m.resultados, " ")
	if !strings.Contains(erro, "cor_favorita") {
		t.Errorf("o modelo devia ter sido avisado do dado inválido: %q", m.resultados)
	}
	coletas, err := s.q.ColetasDaConversa(ctx, res.ConversaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range coletas {
		if c.Campo == "cor_favorita" {
			t.Error("o dado inventado não podia ser gravado")
		}
	}
}
