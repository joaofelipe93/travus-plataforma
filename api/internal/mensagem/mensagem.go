// Package mensagem monta o texto que vai para o WhatsApp do cliente no serviço de boletos
// (issue #48) e normaliza o telefone do cadastro para o formato que o WhatsApp usa.
//
// O texto da cobrança é o que o usuário escreveu na issue #48, palavra por palavra — e está
// fixado por teste de propósito: mudar o texto quebra a suíte, para ninguém mexer sem querer na
// mensagem que chega a um cliente.
package mensagem

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Telefone normaliza o que foi digitado no cadastro para só dígitos com DDI do Brasil.
//
// O cadastro é texto livre ("(11) 90000-0000", "11 90000 0000", "+55 11 90000-0000"), e o
// WhatsApp precisa de DDI + DDD + número. 10 ou 11 dígitos = número nacional, recebe o 55;
// 12 ou 13 dígitos começando com 55 já vêm completos. Qualquer outra coisa é recusada: é
// melhor um aviso de "telefone não dá para usar" do que uma mensagem para o número errado.
func Telefone(bruto string) (string, error) {
	d := digitos(bruto)
	switch {
	case len(d) == 10 || len(d) == 11:
		return "55" + d, nil
	case (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55"):
		return d, nil
	case d == "":
		return "", fmt.Errorf("sem telefone cadastrado")
	default:
		return "", fmt.Errorf("telefone com %d dígitos não dá para usar no WhatsApp: %q", len(d), bruto)
	}
}

func digitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PrimeiroNome: o cadastro guarda o nome inteiro em maiúsculas ("MARIA DA SILVA"), e a mensagem
// fala com a pessoa ("Oi Maria"). Preposição sozinha não serve de nome.
func PrimeiroNome(nome string) string {
	for parte := range strings.FieldsSeq(nome) {
		limpa := strings.Trim(parte, ".,;")
		if limpa == "" {
			continue
		}
		return capitalizar(limpa)
	}
	return ""
}

func capitalizar(palavra string) string {
	runas := []rune(strings.ToLower(palavra))
	if len(runas) == 0 {
		return ""
	}
	runas[0] = unicode.ToUpper(runas[0])
	return string(runas)
}

// Valor formata centavos como o brasileiro escreve: R$ 1.360,50.
func Valor(centavos int64) string {
	negativo := centavos < 0
	if negativo {
		centavos = -centavos
	}
	reais := centavos / 100
	resto := centavos % 100
	texto := fmt.Sprintf("%d", reais)
	var com strings.Builder
	for i, r := range texto {
		if i > 0 && (len(texto)-i)%3 == 0 {
			com.WriteRune('.')
		}
		com.WriteRune(r)
	}
	sinal := ""
	if negativo {
		sinal = "-"
	}
	return fmt.Sprintf("R$ %s%s,%02d", sinal, com.String(), resto)
}

// Vencimento formata a data como dd/mm/aaaa.
func Vencimento(d time.Time) string { return d.Format("02/01/2006") }

// Boleto é o que a mensagem de envio precisa saber de uma parcela.
type Boleto struct {
	Nome       string
	Grupo      string
	Cota       string
	Parcela    string
	Vencimento time.Time
	Centavos   int64
}

// Envio: a mensagem do dia 8, uma por cota, com o boleto anexado.
//
// ATENÇÃO: este texto é um rascunho. A issue #48 só traz o texto da cobrança; este foi proposto
// ao usuário e está esperando confirmação. Trocar aqui é trocar em um lugar só.
func Envio(b Boleto) string {
	parcela := ""
	if p := strings.TrimSpace(b.Parcela); p != "" && p != "0" {
		parcela = fmt.Sprintf(" · Parcela %s", p)
	}
	return fmt.Sprintf(
		"Oi %s, chegou o boleto do seu investimento para construção do seu patrimônio.\n\n"+
			"Grupo %s · Cota %s%s\n"+
			"Vencimento: %s\n"+
			"Valor: %s\n\n"+
			"O boleto está em anexo. Qualquer dúvida, é só chamar aqui. 🤗",
		PrimeiroNome(b.Nome), b.Grupo, b.Cota, parcela, Vencimento(b.Vencimento), Valor(b.Centavos))
}

// Cobranca: a mensagem do dia 14, uma por cliente, sem anexo. Texto da issue #48, literal.
func Cobranca(nome string) string {
	return fmt.Sprintf(
		"Oi %s, vimos que seu investimento para construção do seu patrimônio ainda está disponível para pagamento.\n\n"+
			"Ainda dá tempo de participar da assembleia do mês.\n\n"+
			"Caso já tenha pago, desconsidere essa mensagem. 🤗",
		PrimeiroNome(nome))
}

// Teste: o disparo manual do admin para conferir o número pareado, sem parecer cobrança.
func Teste(quando time.Time) string {
	return fmt.Sprintf("Mensagem de teste da Travus Plataforma (serviço de boletos), em %s. Nenhuma ação é necessária.",
		quando.In(time.FixedZone("-03", -3*3600)).Format("02/01/2006 15:04"))
}
