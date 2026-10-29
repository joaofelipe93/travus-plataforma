package atendimento

import (
	"strings"
	"testing"
	"time"
)

func promptDeTeste(coletado map[string]string) string {
	instr := "Converse em nome da imobiliária e fale só de aluguel."
	etapa := &EtapaPrompt{
		Nome: "Interesse", Objetivo: "Descobrir o que a pessoa procura", Posicao: 1, Total: 2,
		Campos: []Campo{
			{ID: "interesse", Rotulo: "Interesse", Obrigatorio: true},
			{ID: "bairro", Rotulo: "Bairro"},
		},
	}
	return Prompt(BotPrompt{Nome: "Imobiliária X", Instrucoes: instr}, etapa, coletado)
}

func TestPromptTrazOBotAEtapaEOsCampos(t *testing.T) {
	p := promptDeTeste(map[string]string{"bairro": "Centro"})
	for _, esperado := range []string{
		"Imobiliária X",                    // quem ele é
		"fale só de aluguel",               // o que o operador escreveu
		"Etapa atual: Interesse (1 de 2)",  // onde a conversa está
		"Descobrir o que a pessoa procura", // o objetivo
		"interesse (Interesse, obrigatório): ainda não coletado",
		`bairro (Bairro, opcional): já coletado — "Centro"`,
	} {
		if !strings.Contains(p, esperado) {
			t.Errorf("o prompt devia conter %q\n--- prompt ---\n%s", esperado, p)
		}
	}
}

// As regras fixas não são preferência de quem configura: elas valem para todo bot, e é o que
// separa um atendimento de um problema. Se alguém apagar uma, este teste cai.
func TestPromptTemAsRegrasQueProtegemOCliente(t *testing.T) {
	p := promptDeTeste(nil)
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

func TestPromptSemFluxo(t *testing.T) {
	p := Prompt(BotPrompt{Nome: "Bot", Instrucoes: "Responda dúvidas sobre horário."}, nil, nil)
	if !strings.Contains(p, "não tem etapas configuradas") {
		t.Errorf("bot sem fluxo devia dizer isso ao modelo:\n%s", p)
	}
}

// Dado coletado numa etapa anterior precisa aparecer, senão o bot pergunta de novo o que a
// pessoa já respondeu — o jeito mais rápido de parecer um robô ruim.
func TestPromptListaOQueJaSabeDeOutrasEtapas(t *testing.T) {
	p := promptDeTeste(map[string]string{"telefone": "11988887777"})
	if !strings.Contains(p, "Já sabemos desta pessoa") || !strings.Contains(p, "11988887777") {
		t.Errorf("o que foi coletado fora da etapa devia aparecer:\n%s", p)
	}
}

// O prompt fixo vai para o cache: ordem instável quebraria o cache a cada resposta (mapa em Go
// não tem ordem garantida).
func TestPromptEEstavel(t *testing.T) {
	coletado := map[string]string{"telefone": "1", "email": "2", "cidade": "3", "nome": "4"}
	primeiro := promptDeTeste(coletado)
	for i := 0; i < 20; i++ {
		if p := promptDeTeste(coletado); p != primeiro {
			t.Fatal("o prompt mudou de uma montagem para a outra: o cache iria para o lixo")
		}
	}
}

func TestCamposPendentes(t *testing.T) {
	etapa := &EtapaPrompt{Campos: []Campo{
		{ID: "a", Obrigatorio: true}, {ID: "b"}, {ID: "c", Obrigatorio: true},
	}}
	if faltam := CamposPendentes(etapa, map[string]string{"a": "x"}); len(faltam) != 1 || faltam[0] != "c" {
		t.Errorf("devia faltar só o c: %v", faltam)
	}
	// Valor em branco conta como não coletado: "  " não é resposta.
	if faltam := CamposPendentes(etapa, map[string]string{"a": "x", "c": "   "}); len(faltam) != 1 {
		t.Errorf("valor em branco devia continuar pendente: %v", faltam)
	}
	if faltam := CamposPendentes(etapa, map[string]string{"a": "x", "c": "y"}); len(faltam) != 0 {
		t.Errorf("com os obrigatórios coletados não devia faltar nada: %v", faltam)
	}
	// Opcional nunca segura a etapa, e etapa nenhuma não tem pendência.
	if faltam := CamposPendentes(nil, nil); faltam != nil {
		t.Errorf("sem etapa não há pendência: %v", faltam)
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
