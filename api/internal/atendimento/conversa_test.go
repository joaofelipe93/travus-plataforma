package atendimento

import (
	"strings"
	"testing"
	"time"
)

// O prompt do bot. Os exemplos aqui são **genéricos de propósito**: a Travus trabalha com vários
// segmentos (plano de saúde empresarial, consórcio e o que vier), e exemplo de um ramo no teste
// vira exemplo de um ramo no produto.

func perguntasDeTeste(respostas map[string]string) []PerguntaPrompt {
	orientacao := "Se a pessoa não souber, pergunte uma estimativa."
	lista := []PerguntaPrompt{
		{Texto: "Qual é o nome da empresa?", Campo: "nome_da_empresa", Obrigatoria: true},
		{Texto: "Quantas pessoas entrariam?", Campo: "quantas_pessoas", Obrigatoria: true, Orientacao: &orientacao},
		{Texto: "Qual o melhor horário para a equipe ligar?", Campo: "melhor_horario", Obrigatoria: false},
	}
	for i := range lista {
		lista[i].Resposta = respostas[lista[i].Campo]
	}
	return lista
}

// promptDeTeste é o prompt inteiro — as duas partes juntas, que é o que o modelo acaba vendo.
func promptDeTeste(respostas map[string]string, extras map[string]string) string {
	lista := perguntasDeTeste(respostas)
	return PromptEstavel(
		BotPrompt{Nome: "Travus", Instrucoes: "Você é o primeiro atendimento da Travus e fala de plano empresarial."},
		lista,
	) + "\n" + PromptDaVez(lista, extras)
}

func TestPromptListaAsPerguntasNaOrdem(t *testing.T) {
	p := promptDeTeste(map[string]string{"nome_da_empresa": "Acme"}, nil)
	for _, esperado := range []string{
		"Travus",                        // quem ele é
		"fala de plano empresarial",     // o que o operador escreveu
		"1. Qual é o nome da empresa?",  // a ordem é a da lista
		`JÁ RESPONDIDA: "Acme"`,         // o que já se sabe
		"2. Quantas pessoas entrariam?", // a próxima
		"ainda sem resposta [dado: quantas_pessoas]",
		"Orientação: Se a pessoa não souber", // a orientação daquela pergunta
		"Agora trate da pergunta 2",          // a pergunta da vez
	} {
		if !strings.Contains(p, esperado) {
			t.Errorf("o prompt devia conter %q\n--- prompt ---\n%s", esperado, p)
		}
	}
	// A instrução mais importante da nova forma: a pergunta é o que se quer saber, não a frase.
	if !strings.Contains(p, "não copie os textos acima") {
		t.Error("o prompt precisa dizer que o bot reescreve a pergunta com as palavras dele")
	}
}

// As regras fixas não são preferência de quem configura: elas valem para todo bot, e é o que
// separa um atendimento de um problema. Se alguém apagar uma, este teste cai.
func TestPromptTemAsRegrasQueProtegemOCliente(t *testing.T) {
	p := promptDeTeste(nil, nil)
	casos := map[string]string{
		"não finge ser pessoa":  "atendimento automático",
		"não inventa":           "Não invente preço",
		"não pede senha":        "Nunca peça senha",
		"não pede cartão":       "número de cartão",
		"oferece gente de casa": "falar com alguém",
		"escreve curto":         "Escreva curto",
	}
	for nome, trecho := range casos {
		if !strings.Contains(p, trecho) {
			t.Errorf("regra que %s saiu do prompt (esperava %q)", nome, trecho)
		}
	}
}

func TestPromptSemPerguntas(t *testing.T) {
	p := PromptEstavel(BotPrompt{Nome: "Travus", Instrucoes: "Responda dúvidas sobre horário de atendimento."}, nil)
	if !strings.Contains(p, "não tem perguntas configuradas") {
		t.Errorf("bot sem perguntas devia dizer isso ao modelo:\n%s", p)
	}
}

// Com tudo respondido, o prompt manda encerrar — é o que faz a conversa terminar sozinha.
func TestPromptMandaEncerrarNoFim(t *testing.T) {
	p := promptDeTeste(map[string]string{
		"nome_da_empresa": "Acme", "quantas_pessoas": "40", "melhor_horario": "manhã",
	}, nil)
	if !strings.Contains(p, "ferramenta de encerrar") {
		t.Errorf("respondidas todas, o prompt devia mandar encerrar:\n%s", p)
	}
	if strings.Contains(p, "Agora trate da pergunta") {
		t.Error("não há pergunta da vez quando tudo foi respondido")
	}
}

// Pergunta opcional não segura o fim, mas ainda é oferecida enquanto não tiver resposta.
func TestPromptOpcionalNaoSeguraOFim(t *testing.T) {
	respondidas := map[string]string{"nome_da_empresa": "Acme", "quantas_pessoas": "40"}
	lista := perguntasDeTeste(respondidas)
	if faltam := Pendentes(lista); len(faltam) != 0 {
		t.Errorf("as obrigatórias foram respondidas: não devia faltar nada: %v", faltam)
	}
	if proxima := Proxima(lista); proxima == nil || proxima.Campo != "melhor_horario" {
		t.Errorf("a opcional ainda é a próxima a tratar: %+v", proxima)
	}
}

// Dado que a pessoa contou sem ser perguntada precisa aparecer, senão o bot pergunta de novo o
// que ela já respondeu — o jeito mais rápido de parecer um robô ruim.
func TestPromptMostraOQueFoiDitoSemPergunta(t *testing.T) {
	p := promptDeTeste(nil, map[string]string{"cnpj": "00.000.000/0001-00"})
	if !strings.Contains(p, "não pergunte de novo") || !strings.Contains(p, "00.000.000/0001-00") {
		t.Errorf("o que foi registrado fora das perguntas devia aparecer:\n%s", p)
	}
}

// O prompt fixo vai para o cache: ordem instável quebraria o cache a cada resposta (mapa em Go
// não tem ordem garantida).
func TestPromptEEstavel(t *testing.T) {
	extras := map[string]string{"telefone": "1", "email": "2", "cidade": "3", "cnpj": "4"}
	primeiro := promptDeTeste(nil, extras)
	for i := 0; i < 20; i++ {
		if p := promptDeTeste(nil, extras); p != primeiro {
			t.Fatal("o prompt mudou de uma montagem para a outra: o cache iria para o lixo")
		}
	}
}

// Proxima é o que substituiu o controle de etapa: não há estado de "onde a conversa está".
func TestProximaEhAPrimeiraSemResposta(t *testing.T) {
	if proxima := Proxima(perguntasDeTeste(nil)); proxima == nil || proxima.Posicao != 1 {
		t.Errorf("sem nada respondido, a próxima é a primeira: %+v", proxima)
	}
	lista := perguntasDeTeste(map[string]string{"nome_da_empresa": "Acme"})
	if proxima := Proxima(lista); proxima == nil || proxima.Campo != "quantas_pessoas" {
		t.Errorf("respondida a primeira, a próxima é a segunda: %+v", proxima)
	}
	// Resposta em branco não conta como respondida: "  " não é resposta.
	lista = perguntasDeTeste(map[string]string{"nome_da_empresa": "   "})
	if proxima := Proxima(lista); proxima == nil || proxima.Posicao != 1 {
		t.Errorf("resposta em branco devia continuar pendente: %+v", proxima)
	}
	todas := perguntasDeTeste(map[string]string{
		"nome_da_empresa": "Acme", "quantas_pessoas": "40", "melhor_horario": "manhã",
	})
	if proxima := Proxima(todas); proxima != nil {
		t.Errorf("com tudo respondido não há próxima: %+v", proxima)
	}
	if Proxima(nil) != nil {
		t.Error("bot sem perguntas não tem próxima")
	}
}

func TestPendentesSoContaObrigatorias(t *testing.T) {
	faltam := Pendentes(perguntasDeTeste(nil))
	if len(faltam) != 2 {
		t.Fatalf("as duas obrigatórias deviam faltar: %v", faltam)
	}
	if faltam[0] != "nome_da_empresa" || faltam[1] != "quantas_pessoas" {
		t.Errorf("faltou na ordem das perguntas: %v", faltam)
	}
	if faltam := Pendentes(nil); faltam != nil {
		t.Errorf("sem perguntas não há pendência: %v", faltam)
	}
}

func TestMotivoValido(t *testing.T) {
	for _, m := range MotivosEncerramento {
		if !MotivoValido(m) {
			t.Errorf("%q devia ser válido", m)
		}
	}
	if MotivoValido("cansei") {
		t.Error("motivo inventado não devia passar: a tela agrupa por esses valores")
	}
}

func TestContexto(t *testing.T) {
	agora := time.Date(2026, 10, 29, 15, 4, 0, 0, time.UTC)
	c := Contexto(agora, "Fulano")
	if !strings.Contains(c, "29/10/2026 15:04") {
		t.Errorf("o contexto devia trazer data e hora: %q", c)
	}
	if !strings.Contains(c, "Fulano") || !strings.Contains(c, "confirme antes de usar") {
		t.Errorf("o nome do WhatsApp devia vir com a ressalva de confirmar: %q", c)
	}
	if strings.Contains(Contexto(agora, "   "), "está no nome de") {
		t.Error("sem nome de contato, não devia inventar a linha")
	}
}

// O ponto da divisão do prompt (issue #128, item dos tokens): o bloco cacheado **não muda** quando
// a pessoa responde algo. Se este teste cair, o cache do prompt voltou a morrer a cada resposta e
// a conta volta a crescer sem ninguém notar.
func TestPromptEstavelNaoMudaQuandoAPessoaResponde(t *testing.T) {
	bot := BotPrompt{Nome: "Travus", Instrucoes: "Primeiro atendimento."}
	vazio := PromptEstavel(bot, perguntasDeTeste(nil))
	comUma := PromptEstavel(bot, perguntasDeTeste(map[string]string{"nome_da_empresa": "Acme"}))
	comDuas := PromptEstavel(bot, perguntasDeTeste(map[string]string{
		"nome_da_empresa": "Acme", "quantas_pessoas": "40",
	}))
	if vazio != comUma || vazio != comDuas {
		t.Error("o bloco cacheado mudou com as respostas: o cache do prompt morre a cada resposta")
	}
	// E ele não leva o que a pessoa disse — isso é da parte da vez.
	if strings.Contains(comDuas, "Acme") || strings.Contains(comDuas, "JÁ RESPONDIDA") {
		t.Errorf("o bloco cacheado não devia conter resposta nenhuma:\n%s", comDuas)
	}
	// A parte da vez, sim, muda e diz o que já se sabe.
	daVez := PromptDaVez(perguntasDeTeste(map[string]string{"nome_da_empresa": "Acme"}), nil)
	for _, esperado := range []string{`JÁ RESPONDIDA: "Acme"`, "Agora trate da pergunta 2"} {
		if !strings.Contains(daVez, esperado) {
			t.Errorf("a parte da vez devia conter %q:\n%s", esperado, daVez)
		}
	}
	if PromptDaVez(perguntasDeTeste(nil), nil) == daVez {
		t.Error("a parte da vez devia mudar com as respostas")
	}
}

// O bloco cacheado precisa passar do mínimo que a API exige para cachear (512 tokens no Haiku
// 5.5); abaixo disso o cache simplesmente não acontece, sem erro nenhum. Não dá para contar token
// aqui sem chamar a API, então o teste guarda a ordem de grandeza em caracteres — com um bot de
// instruções curtas, as regras fixas e a lista de perguntas já põem o bloco acima disso.
func TestPromptEstavelTemTamanhoCacheavel(t *testing.T) {
	p := PromptEstavel(BotPrompt{Nome: "Travus", Instrucoes: "Primeiro atendimento."}, perguntasDeTeste(nil))
	if len(p) < 2000 {
		t.Errorf("o bloco cacheado tem %d caracteres: perto do mínimo para o cache pegar", len(p))
	}
}

// O nome do bot pode ser de pessoa (o bot da Smart se chama Rodrigo). O prompt tem de dizer que
// ESSE é o nome dele, e não que existe alguém com esse nome por trás: antes dizia "você é o
// atendimento de Rodrigo", e em produção o modelo respondeu "não faço parte de uma organização
// como funcionário, então não tenho cargo" quando perguntaram quem era.
func TestPromptDizQueONomeEODoProprioBot(t *testing.T) {
	p := PromptEstavel(BotPrompt{Nome: "Rodrigo", Instrucoes: "Primeiro atendimento da Smart."}, nil)
	if !strings.Contains(p, "se apresenta como Rodrigo") {
		t.Errorf("o prompt devia dizer que o bot se apresenta com esse nome:\n%s", p)
	}
	if strings.Contains(p, "atendimento de Rodrigo") {
		t.Error("essa forma faz o modelo tratar o nome como se fosse outra pessoa")
	}
	// E continua dizendo que não é gente.
	if !strings.Contains(p, "atendimento automático") {
		t.Error("o prompt precisa dizer que é atendimento automático")
	}
}
