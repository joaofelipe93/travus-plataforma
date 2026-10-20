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
func TestPadroesDizemQueEhAutomatica(t *testing.T) {
	// O número dos boletos não tem atendente (decisão do usuário em 20/10/2026): as duas
	// mensagens precisam avisar isso, e nenhuma pode convidar o cliente a responder.
	for nome, texto := range map[string]string{"envio": PadraoEnvio, "cobrança": PadraoCobranca} {
		if !strings.Contains(texto, "mensagem automática") {
			t.Errorf("a mensagem de %s não diz que é automática:\n%s", nome, texto)
		}
		if !strings.Contains(texto, "não recebe respostas") {
			t.Errorf("a mensagem de %s não avisa que ninguém lê a resposta:\n%s", nome, texto)
		}
		for _, convite := range []string{"é só chamar", "chame aqui", "responda"} {
			if strings.Contains(strings.ToLower(texto), convite) {
				t.Errorf("a mensagem de %s convida a responder (%q):\n%s", nome, convite, texto)
			}
		}
	}
	// A mesma mensagem para todos: o padrão não trata pelo nome (o campo existe, mas é opcional).
	for nome, texto := range map[string]string{"envio": PadraoEnvio, "cobrança": PadraoCobranca} {
		if strings.Contains(texto, "{nome}") {
			t.Errorf("o padrão de %s não devia personalizar: %s", nome, texto)
		}
	}
}

func TestCobrancaMantemOTextoDaIssue(t *testing.T) {
	// As frases que o usuário escreveu na issue #48 continuam de pé; só o fim mudou.
	for _, pedaco := range []string{
		"ainda está disponível para pagamento",
		"Ainda dá tempo de participar da assembleia do mês",
		"Caso já tenha pago, desconsidere esta mensagem",
	} {
		if !strings.Contains(Cobranca(PadraoCobranca, "FELIPE MACHADO"), pedaco) {
			t.Errorf("a cobrança perdeu %q", pedaco)
		}
	}
}

func TestCobrancaComNome(t *testing.T) {
	modelo := "Oi {nome}, tudo bem?"
	if got := Cobranca(modelo, "FELIPE MACHADO"); got != "Oi Felipe, tudo bem?" {
		t.Errorf("Cobranca() = %q", got)
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
	got := Envio(PadraoEnvio, b)
	for _, pedaco := range []string{
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
	if strings.Contains(Envio(PadraoEnvio, b), "Parcela 0") {
		t.Error("parcela zero não devia aparecer")
	}
}

func TestEnvioNaoVazaDocumento(t *testing.T) {
	// Nenhuma mensagem leva CPF: o cliente sabe o dele, e o texto circula em print de tela.
	b := Boleto{Nome: "MARIA", Grupo: "006650", Cota: "0924", Vencimento: time.Now(), Centavos: 100}
	for _, texto := range []string{Envio(PadraoEnvio, b), Cobranca(PadraoCobranca, "MARIA"), Teste(time.Now())} {
		for _, proibido := range []string{"CPF", "cpf", "11122233396"} {
			if strings.Contains(texto, proibido) {
				t.Errorf("mensagem com %q: %s", proibido, texto)
			}
		}
	}
}

func TestValidar(t *testing.T) {
	if err := Validar(PadraoEnvio, CamposEnvio); err != nil {
		t.Errorf("o padrão de envio devia passar: %v", err)
	}
	if err := Validar(PadraoCobranca, CamposCobranca); err != nil {
		t.Errorf("o padrão de cobrança devia passar: %v", err)
	}
	// Na cobrança só existe {nome}: {vencimento} é de outra mensagem e precisa ser recusado.
	recusados := map[string]struct {
		modelo string
		campos []Campo
	}{
		"vazio":           {"   \n ", CamposCobranca},
		"campo inventado": {"Oi {nmoe}, chegou o boleto", CamposEnvio},
		"campo de outra":  {"Vence em {vencimento}", CamposCobranca},
		"longo demais":    {strings.Repeat("a", MaxTexto+1), CamposCobranca},
	}
	for nome, caso := range recusados {
		if err := Validar(caso.modelo, caso.campos); err == nil {
			t.Errorf("%s devia ser recusado", nome)
		}
	}
	// O erro ensina: diz qual campo não existe e quais existem.
	err := Validar("Oi {nmoe}", CamposCobranca)
	if err == nil || !strings.Contains(err.Error(), "{nmoe}") || !strings.Contains(err.Error(), "{nome}") {
		t.Errorf("o erro devia citar o campo errado e os certos: %v", err)
	}
}

func TestRenderizarCampoVazio(t *testing.T) {
	b := Boleto{
		Nome: "MARIA DA SILVA", Grupo: "006650", Cota: "0924", Parcela: "",
		Vencimento: time.Date(2026, time.November, 16, 0, 0, 0, 0, time.UTC), Centavos: 64462,
	}
	got := Envio(PadraoEnvio, b)
	// Sem parcela, o pedaço some junto com o separador: nada de "Parcela" solta nem " · " no fim.
	if !strings.Contains(got, "Grupo 006650 · Cota 0924\n") {
		t.Errorf("a linha da cota devia terminar na cota:\n%s", got)
	}
	if strings.Contains(got, "Parcela") || strings.Contains(got, "· \n") || strings.Contains(got, " ·\n") {
		t.Errorf("sobrou pedaço vazio na mensagem:\n%s", got)
	}
	// Com parcela, ela aparece.
	b.Parcela = "018"
	if !strings.Contains(Envio(PadraoEnvio, b), "Grupo 006650 · Cota 0924 · Parcela 018") {
		t.Error("a parcela devia aparecer quando o site informa")
	}
}

func TestRenderizarLinhaInteiraDependente(t *testing.T) {
	// A linha que só tem o campo vazio some, em vez de virar uma linha em branco no meio.
	modelo := "Olá!\n\nParcela {parcela}\n\nAté mais."
	got := Renderizar(modelo, map[string]string{"parcela": ""})
	if strings.Contains(got, "Parcela") {
		t.Errorf("a linha devia sumir:\n%s", got)
	}
	if got != "Olá!\n\nAté mais." {
		t.Errorf("Renderizar() = %q", got)
	}
}
