package mensagem

import (
	"strings"
	"testing"
	"time"
)

func TestTelefone(t *testing.T) {
	validos := map[string]string{
		"(11) 90000-0000":   "5511900000000",
		"11 90000 0000":     "5511900000000",
		"+55 11 90000-0000": "5511900000000",
		"5511900000000":     "5511900000000",
		"1130000000":        "551130000000", // fixo, 10 dígitos
	}
	for bruto, esperado := range validos {
		got, err := Telefone(bruto)
		if err != nil {
			t.Errorf("Telefone(%q): %v", bruto, err)
			continue
		}
		if got != esperado {
			t.Errorf("Telefone(%q) = %q, quer %q", bruto, got, esperado)
		}
	}
	for _, bruto := range []string{"", "   ", "123", "900000000", "1 (11) 90000-0000", "00 11 90000-0000"} {
		if got, err := Telefone(bruto); err == nil {
			t.Errorf("Telefone(%q) = %q, devia recusar", bruto, got)
		}
	}
}

func TestPrimeiroNome(t *testing.T) {
	casos := map[string]string{
		"MARIA DA SILVA":      "Maria",
		"joão machado":        "João",
		"  ANA  ":             "Ana",
		"EXEMPLO, CLIENTE UM": "Exemplo",
		"":                    "",
	}
	for nome, esperado := range casos {
		if got := PrimeiroNome(nome); got != esperado {
			t.Errorf("PrimeiroNome(%q) = %q, quer %q", nome, got, esperado)
		}
	}
}

func TestValor(t *testing.T) {
	casos := map[int64]string{
		64462:   "R$ 644,62",
		136050:  "R$ 1.360,50",
		100:     "R$ 1,00",
		5:       "R$ 0,05",
		1234567: "R$ 12.345,67",
	}
	for centavos, esperado := range casos {
		if got := Valor(centavos); got != esperado {
			t.Errorf("Valor(%d) = %q, quer %q", centavos, got, esperado)
		}
	}
}

// O texto da cobrança é o da issue #48, literal. Se este teste falhar, a mensagem que chega ao
// cliente mudou: confirme com o usuário antes de ajustar o esperado.
func TestCobrancaTextoDaIssue(t *testing.T) {
	esperado := "Oi Felipe, vimos que seu investimento para construção do seu patrimônio ainda está disponível para pagamento.\n\n" +
		"Ainda dá tempo de participar da assembleia do mês.\n\n" +
		"Caso já tenha pago, desconsidere essa mensagem. 🤗"
	if got := Cobranca("FELIPE MACHADO"); got != esperado {
		t.Errorf("Cobranca():\n%s\n\nquer:\n%s", got, esperado)
	}
}

func TestEnvio(t *testing.T) {
	b := Boleto{
		Nome:       "MARIA DA SILVA",
		Grupo:      "006650",
		Cota:       "0924",
		Parcela:    "018",
		Vencimento: time.Date(2026, time.November, 16, 0, 0, 0, 0, time.UTC),
		Centavos:   64462,
	}
	got := Envio(b)
	for _, pedaco := range []string{
		"Oi Maria,",
		"Grupo 006650 · Cota 0924 · Parcela 018",
		"Vencimento: 16/11/2026",
		"Valor: R$ 644,62",
		"em anexo",
	} {
		if !strings.Contains(got, pedaco) {
			t.Errorf("mensagem de envio sem %q:\n%s", pedaco, got)
		}
	}
	// A linha "DIF" não é número de parcela: sem o rótulo de parcela fica estranho, mas sem o
	// valor é pior. O que não pode é a mensagem citar "Parcela 0".
	b.Parcela = "0"
	if strings.Contains(Envio(b), "Parcela 0") {
		t.Error("parcela zero não devia aparecer")
	}
}

func TestEnvioNaoVazaDocumento(t *testing.T) {
	// Nenhuma mensagem leva CPF: o cliente sabe o dele, e o texto circula em print de tela.
	b := Boleto{Nome: "MARIA", Grupo: "006650", Cota: "0924", Vencimento: time.Now(), Centavos: 100}
	for _, texto := range []string{Envio(b), Cobranca("MARIA"), Teste(time.Now())} {
		for _, proibido := range []string{"CPF", "cpf", "11122233396"} {
			if strings.Contains(texto, proibido) {
				t.Errorf("mensagem com %q: %s", proibido, texto)
			}
		}
	}
}
