// Package mensagem monta o texto que vai para o WhatsApp do cliente no serviço de boletos
// (issue #48) e normaliza o telefone do cadastro para o formato que o WhatsApp usa.
//
// O texto é **um modelo configurável** (tela Boletos → Configuração, migração 00019): uma
// mensagem padrão para todos, com campos entre chaves que a plataforma preenche na hora do
// envio — ninguém edita mensagem cliente a cliente. Os padrões daqui são o que a migração
// grava e o que o botão "voltar ao texto padrão" devolve, e estão fixados por teste: mudar um
// deles sem querer quebra a suíte, porque é texto que chega a um cliente.
//
// Os dois padrões dizem que a mensagem é automática e que o número não recebe resposta
// (decisão do usuário em 20/10/2026): não há atendente nesse WhatsApp.
package mensagem

import (
	"fmt"
	"regexp"
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

// Campo é um campo que a plataforma preenche no modelo. O que a tela mostra ao lado da caixa
// de texto sai daqui, então a descrição é escrita para quem vai usar, não para quem programa.
type Campo struct {
	// Nome sem as chaves: "grupo" para {grupo}.
	Nome      string `json:"nome"`
	Descricao string `json:"descricao"`
	// Exemplo é o que entra na prévia da tela.
	Exemplo string `json:"exemplo"`
}

// CamposEnvio: a mensagem do dia 8 fala de uma parcela de uma cota.
var CamposEnvio = []Campo{
	{Nome: "grupo", Descricao: "Grupo da cota", Exemplo: "006650"},
	{Nome: "cota", Descricao: "Número da cota", Exemplo: "0924"},
	{Nome: "parcela", Descricao: "Número da parcela (o site nem sempre informa)", Exemplo: "018"},
	{Nome: "vencimento", Descricao: "Vencimento do boleto, dd/mm/aaaa", Exemplo: "16/11/2026"},
	{Nome: "valor", Descricao: "Valor do boleto", Exemplo: "R$ 644,62"},
	{Nome: "nome", Descricao: "Primeiro nome do cliente — a mensagem é a mesma para todos; use só se quiser tratar pelo nome", Exemplo: "Maria"},
}

// CamposCobranca: a cobrança é uma por cliente e não fala de uma parcela específica.
var CamposCobranca = []Campo{
	{Nome: "nome", Descricao: "Primeiro nome do cliente — a mensagem é a mesma para todos; use só se quiser tratar pelo nome", Exemplo: "Maria"},
}

// Os textos que a plataforma usa enquanto ninguém editar. Impessoais de propósito (é a mesma
// mensagem para todos) e deixando claro que ninguém lê as respostas.
const (
	PadraoEnvio = "Olá! Chegou o boleto do seu investimento para construção do seu patrimônio.\n\n" +
		"Grupo {grupo} · Cota {cota} · Parcela {parcela}\n" +
		"Vencimento: {vencimento}\n" +
		"Valor: {valor}\n\n" +
		"O boleto está em anexo.\n\n" +
		"Esta é uma mensagem automática: este número não recebe respostas."

	PadraoCobranca = "Olá! Vimos que seu investimento para construção do seu patrimônio ainda está disponível para pagamento.\n\n" +
		"Ainda dá tempo de participar da assembleia do mês.\n\n" +
		"Caso já tenha pago, desconsidere esta mensagem.\n\n" +
		"Esta é uma mensagem automática: este número não recebe respostas."
)

// MaxTexto: um WhatsApp aceita bem mais, mas mensagem longa no privado de cliente é o caminho
// curto para denúncia de spam — e o limite mantém o que é gravado por mensagem sob controle.
const MaxTexto = 1000

// Envio: a mensagem do dia 8, uma por cota, com o boleto anexado.
func Envio(modelo string, b Boleto) string {
	return Renderizar(modelo, map[string]string{
		"nome":       PrimeiroNome(b.Nome),
		"grupo":      b.Grupo,
		"cota":       b.Cota,
		"parcela":    parcelaExibivel(b.Parcela),
		"vencimento": Vencimento(b.Vencimento),
		"valor":      Valor(b.Centavos),
	})
}

// Cobranca: a mensagem do dia 14, uma por cliente, sem anexo.
func Cobranca(modelo, nome string) string {
	return Renderizar(modelo, map[string]string{"nome": PrimeiroNome(nome)})
}

// parcelaExibivel: a linha "DIF" (reajuste) e a parcela "0" não são número de parcela.
func parcelaExibivel(p string) string {
	limpa := strings.TrimSpace(p)
	if limpa == "0" {
		return ""
	}
	return limpa
}

var chaves = regexp.MustCompile(`\{([^{}]*)\}`)

// Validar recusa o modelo antes de ele virar mensagem para cliente: texto vazio, longo demais
// ou com campo que a plataforma não sabe preencher ({nmoe} em vez de {nome}). É melhor o erro
// aparecer no botão Salvar do que no WhatsApp de alguém.
func Validar(modelo string, campos []Campo) error {
	limpo := strings.TrimSpace(modelo)
	if limpo == "" {
		return fmt.Errorf("escreva o texto da mensagem")
	}
	if len([]rune(limpo)) > MaxTexto {
		return fmt.Errorf("o texto passa de %d caracteres", MaxTexto)
	}
	conhecidos := make(map[string]bool, len(campos))
	nomes := make([]string, 0, len(campos))
	for _, c := range campos {
		conhecidos[c.Nome] = true
		nomes = append(nomes, "{"+c.Nome+"}")
	}
	for _, achado := range chaves.FindAllStringSubmatch(limpo, -1) {
		if !conhecidos[strings.TrimSpace(achado[1])] {
			return fmt.Errorf("o texto usa %s, que não existe; os campos desta mensagem são %s",
				achado[0], strings.Join(nomes, ", "))
		}
	}
	return nil
}

// Renderizar troca os campos pelos valores.
//
// A sutileza é o campo vazio (na prática, a parcela: o site nem sempre informa). O pedaço da
// linha que dependia dele some junto com o separador " · ", e a linha some inteira se não
// sobrar nada — senão a mensagem sairia com "Parcela" solta ou um " · " no fim. Nenhum campo
// vazio deixa buraco no meio do texto.
func Renderizar(modelo string, valores map[string]string) string {
	linhas := strings.Split(strings.TrimSpace(modelo), "\n")
	saida := make([]string, 0, len(linhas))
	for _, linha := range linhas {
		pedacos := strings.Split(linha, "·")
		mantidos := make([]string, 0, len(pedacos))
		for _, pedaco := range pedacos {
			texto, vazio := substituir(pedaco, valores)
			if vazio {
				continue
			}
			mantidos = append(mantidos, strings.TrimSpace(texto))
		}
		if len(pedacos) > 0 && len(mantidos) == 0 {
			continue // a linha inteira dependia de um campo vazio
		}
		saida = append(saida, strings.Join(mantidos, " · "))
	}
	// Linha removida no meio não deixa um vão de três ou mais quebras.
	texto := strings.Join(saida, "\n")
	for strings.Contains(texto, "\n\n\n") {
		texto = strings.ReplaceAll(texto, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(texto)
}

// substituir troca os campos de um pedaço e avisa se algum deles veio vazio.
func substituir(pedaco string, valores map[string]string) (string, bool) {
	vazio := false
	texto := chaves.ReplaceAllStringFunc(pedaco, func(achado string) string {
		nome := strings.TrimSpace(achado[1 : len(achado)-1])
		valor, conhecido := valores[nome]
		if !conhecido {
			// Validar não deixa chegar aqui; se chegar, o campo fica como está, visível.
			return achado
		}
		if strings.TrimSpace(valor) == "" {
			vazio = true
		}
		return valor
	})
	return texto, vazio
}

// Teste: o disparo manual do admin para conferir o número pareado, sem parecer cobrança.
func Teste(quando time.Time) string {
	return fmt.Sprintf("Mensagem de teste da Travus Plataforma (serviço de boletos), em %s. Nenhuma ação é necessária.",
		quando.In(time.FixedZone("-03", -3*3600)).Format("02/01/2006 15:04"))
}
