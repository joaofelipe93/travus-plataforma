// Package cadastro valida e normaliza o que o admin/operador digita no CRM do Canopus
// (clientes e cotas). As mesmas regras da planilha antiga, agora aplicadas no formulário:
// nome em maiúsculas com espaços únicos, grupo/cota/versão só com dígitos e zeros à esquerda
// (6/4/2). O pacote importacao repete essas regras de propósito: lá elas são a paridade com o
// csv.js do script legado e não podem mudar; aqui elas acompanham o formulário.
package cadastro

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Erro é um problema no que foi digitado: vira 400 com a mensagem para quem preencheu.
type Erro struct{ Mensagem string }

func (e Erro) Error() string { return e.Mensagem }

func erro(formato string, args ...any) error { return Erro{fmt.Sprintf(formato, args...)} }

// Modalidades aceitas na tela de credenciamento do Newcon (mesma lista do CHECK da tabela).
var Modalidades = []string{"livre", "fixo", "segundo_fixo", "limitado", "fidelidade"}

const (
	maxNome           = 200
	maxTexto          = 100
	ModalidadePadrao  = "segundo_fixo"
	AdministradoraPad = "CANOPUS"
)

// NormalizarNome identifica o cliente: maiúsculas e espaços únicos (a planilha não tem CPF).
func NormalizarNome(nome string) string {
	return strings.ToUpper(strings.Join(strings.Fields(nome), " "))
}

// Nome devolve o nome como fica guardado (espaços únicos) e o normalizado da chave.
func Nome(bruto string) (nome, normalizado string, err error) {
	nome = strings.Join(strings.Fields(bruto), " ")
	if nome == "" {
		return "", "", erro("informe o nome do cliente")
	}
	if len([]rune(nome)) > maxNome {
		return "", "", erro("nome muito longo (máximo de %d caracteres)", maxNome)
	}
	return nome, NormalizarNome(nome), nil
}

// Texto opcional: espaços aparados, vazio vira nulo.
func Texto(campo, bruto string) (*string, error) {
	v := strings.Join(strings.Fields(bruto), " ")
	if v == "" {
		return nil, nil
	}
	if len([]rune(v)) > maxTexto {
		return nil, erro("%s muito longo (máximo de %d caracteres)", campo, maxTexto)
	}
	return &v, nil
}

// TextoMaiusculo é o Texto de campos que o Newcon e a planilha usam em caixa alta
// (administradora, tipo de consórcio, forma de pagamento).
func TextoMaiusculo(campo, bruto string) (*string, error) {
	v, err := Texto(campo, bruto)
	if err != nil || v == nil {
		return v, err
	}
	m := strings.ToUpper(*v)
	return &m, nil
}

// Email aceita vazio; o resto precisa parecer um e-mail (uma arroba, com texto dos dois lados).
func Email(bruto string) (*string, error) {
	v := strings.TrimSpace(bruto)
	if v == "" {
		return nil, nil
	}
	if len([]rune(v)) > maxTexto {
		return nil, erro("e-mail muito longo (máximo de %d caracteres)", maxTexto)
	}
	antes, depois, achou := strings.Cut(v, "@")
	if !achou || antes == "" || depois == "" || strings.Contains(depois, "@") ||
		!strings.Contains(depois, ".") || strings.ContainsFunc(v, unicode.IsSpace) {
		return nil, erro("e-mail inválido: %q", bruto)
	}
	return &v, nil
}

// Grupo, Cota e Versao: só dígitos, com zeros à esquerda (006650, 0924, 00).
func Grupo(bruto string) (string, error) { return numero("grupo", bruto, 6, false) }
func Cota(bruto string) (string, error)  { return numero("cota", bruto, 4, false) }

func Versao(bruto string) (string, error) {
	if strings.TrimSpace(bruto) == "" {
		return "00", nil
	}
	return numero("versão", bruto, 2, true)
}

func numero(campo, bruto string, largura int, zeroVale bool) (string, error) {
	d := digitos(bruto)
	if d == "" {
		return "", erro("informe %s (só números)", campo)
	}
	if len(d) > largura {
		return "", erro("%s com mais de %d dígitos: %q", campo, largura, bruto)
	}
	v := strings.Repeat("0", largura-len(d)) + d
	if !zeroVale && strings.Trim(v, "0") == "" {
		return "", erro("%s inválido: %q", campo, bruto)
	}
	return v, nil
}

// Modalidade da tela de credenciamento; vazio vira o padrão (2º Fixo).
func Modalidade(bruto string) (string, error) {
	v := strings.TrimSpace(strings.ToLower(bruto))
	if v == "" {
		return ModalidadePadrao, nil
	}
	if slices.Contains(Modalidades, v) {
		return v, nil
	}
	return "", erro("modalidade inválida: %q (use %s)", bruto, strings.Join(Modalidades, ", "))
}

// DiaDoMes aceita vazio (nulo) ou 1 a 31: vencimento da parcela e dia da assembleia.
func DiaDoMes(campo string, bruto *int) (*int16, error) {
	if bruto == nil {
		return nil, nil
	}
	if *bruto < 1 || *bruto > 31 {
		return nil, erro("%s precisa ser um dia entre 1 e 31 (recebido %d)", campo, *bruto)
	}
	d := int16(*bruto)
	return &d, nil
}

// Data aceita vazio (nulo) ou AAAA-MM-DD, o formato que o campo de data da web manda.
func Data(campo, bruto string) (*time.Time, error) {
	v := strings.TrimSpace(bruto)
	if v == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return nil, erro("%s inválida: use o formato AAAA-MM-DD (recebido %q)", campo, bruto)
	}
	return &d, nil
}

// Administradora: vazio vira CANOPUS, o único formato que o worker sabe operar hoje.
func Administradora(bruto string) (string, error) {
	v, err := TextoMaiusculo("administradora", bruto)
	if err != nil {
		return "", err
	}
	if v == nil {
		return AdministradoraPad, nil
	}
	return *v, nil
}

// CpfCnpj aceita vazio (nulo) ou o documento com 11 (CPF) ou 14 (CNPJ) dígitos, guardado só
// com dígitos — o formato que o Boleto Fácil do Canopus exige em /find-cota/{documento}.
// Os dígitos verificadores são conferidos aqui porque documento errado não dá erro no site:
// ele devolve uma lista vazia de cotas e, depois de três consultas sem resultado, pede captcha.
func CpfCnpj(bruto string) (*string, error) {
	if strings.TrimSpace(bruto) == "" {
		return nil, nil
	}
	d := digitos(bruto)
	switch len(d) {
	case 11:
		if !cpfValido(d) {
			return nil, erro("CPF inválido: %q", bruto)
		}
	case 14:
		if !cnpjValido(d) {
			return nil, erro("CNPJ inválido: %q", bruto)
		}
	default:
		return nil, erro("CPF/CNPJ precisa ter 11 ou 14 dígitos (recebido %d em %q)", len(d), bruto)
	}
	return &d, nil
}

// cpfValido confere os dois dígitos verificadores. Documento de dígitos todos iguais
// (00000000000, 11111111111…) passa na conta e é recusado à parte, como manda a Receita.
func cpfValido(d string) bool {
	if todosIguais(d) {
		return false
	}
	for _, c := range [2]struct{ ate, peso int }{{9, 10}, {10, 11}} {
		soma := 0
		for i := 0; i < c.ate; i++ {
			soma += int(d[i]-'0') * (c.peso - i)
		}
		if int(d[c.ate]-'0') != verificador(soma) {
			return false
		}
	}
	return true
}

// cnpjValido confere os dois dígitos verificadores (pesos de 2 a 9, da direita para a esquerda).
func cnpjValido(d string) bool {
	if todosIguais(d) {
		return false
	}
	pesos := [2][]int{
		{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2},
		{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2},
	}
	for i, p := range pesos {
		soma := 0
		for j, peso := range p {
			soma += int(d[j]-'0') * peso
		}
		if int(d[12+i]-'0') != verificador(soma) {
			return false
		}
	}
	return true
}

func verificador(soma int) int {
	resto := soma % 11
	if resto < 2 {
		return 0
	}
	return 11 - resto
}

func todosIguais(d string) bool {
	return strings.Trim(d, d[:1]) == ""
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
