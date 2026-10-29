// Package atendimento guarda as regras do serviço de Atendimento (issue #100): o catálogo de
// modelos que um bot pode usar e a validação do que a tela manda.
//
// Por que o modelo é texto no banco e lista aqui: um CHECK no Postgres obrigaria a uma migração
// a cada modelo novo da Anthropic, e migração existente não se edita (regra da CI). Aqui a lista
// muda com um PR normal.
package atendimento

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cadastro"
)

// Modelo que um bot pode usar, como a tela mostra.
type Modelo struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
	// Uma frase: quando escolher este. A tela mostra ao lado do nome.
	Descricao string `json:"descricao"`
}

// ModeloPadrao é o que um bot novo recebe: um atendimento é conversa curta e de volume alto, e
// o Haiku é a ordem de grandeza certa para isso. Opus é escolha consciente, não padrão.
const ModeloPadrao = "claude-haiku-5-5"

// Modelos aceitos, na ordem em que a tela mostra (do mais barato ao mais capaz).
//
// A proporção de custo entre eles, nos preços de 06/10/2026, era de cerca de 1 : 20 : 40 por
// token de entrada (Haiku 5.5, Sonnet 5.5, Opus 5.5). Não escrevo valor em real nem em dólar
// aqui porque preço muda e código envelhece mal; a tela fala em "mais barato" e "mais capaz".
var Modelos = []Modelo{
	{
		ID:        "claude-haiku-5-5",
		Nome:      "Haiku 5.5",
		Descricao: "O mais rápido e o mais barato. Dá conta de atendimento com fluxo definido, que é o caso da maioria dos bots.",
	},
	{
		ID:        "claude-sonnet-5-5",
		Nome:      "Sonnet 5.5",
		Descricao: "Entende pedido confuso e assunto técnico melhor que o Haiku, por algumas vezes o custo dele.",
	},
	{
		ID:        "claude-opus-5-5",
		Nome:      "Opus 5.5",
		Descricao: "O mais capaz, para atendimento que decide coisa difícil. É o mais caro: escolha com o volume do bot em mente.",
	},
}

// ModeloValido diz se o id é de um modelo que a plataforma aceita num bot.
func ModeloValido(id string) bool {
	for _, m := range Modelos {
		if m.ID == id {
			return true
		}
	}
	return false
}

// Limites do que a tela pode mandar. São generosos: existem para barrar absurdo (instrução de
// 1 MB, 500 etapas) e para a mensagem de erro ser clara, não para apertar quem escreve.
const (
	MaxNome       = 80
	MaxInstrucoes = 20000
	MinInstrucoes = 20
	MaxSaudacao   = 1000
	MaxEtapas     = 30
	MaxObjetivo   = 500
	MinObjetivo   = 10
	MaxCampos     = 20
	MaxRotulo     = 60
)

// O slug é a pasta da sessão do WhatsApp no volume do worker e o id do bot nas rotas internas:
// daí só minúscula, dígito e hífen. Mesma expressão do CHECK da migração 00020.
var reSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// O id de um campo coletado vira chave em atendimento.coletas e nome de argumento na ferramenta
// que o modelo chama: sem espaço, sem acento, sem hífen.
var reCampoID = regexp.MustCompile(`^[a-z][a-z0-9_]{1,30}$`)

func erro(formato string, args ...any) error {
	return cadastro.Erro{Mensagem: fmt.Sprintf(formato, args...)}
}

// Campo que uma etapa precisa coletar. Vira argumento da ferramenta `registrar_dado` e chave em
// atendimento.coletas.
type Campo struct {
	ID          string `json:"id"`
	Rotulo      string `json:"rotulo"`
	Obrigatorio bool   `json:"obrigatorio"`
}

// Texto obrigatório, com limite de tamanho. Devolve o valor já sem espaço nas pontas.
func texto(campo, bruto string, minimo, maximo int) (string, error) {
	v := strings.TrimSpace(bruto)
	if utf8.RuneCountInString(v) < minimo {
		if minimo <= 1 {
			return "", erro("%s é obrigatório", campo)
		}
		return "", erro("%s precisa ter ao menos %d caracteres", campo, minimo)
	}
	if utf8.RuneCountInString(v) > maximo {
		return "", erro("%s passa de %d caracteres", campo, maximo)
	}
	return v, nil
}

// Opcional devolve nil quando vazio: campo em branco apaga o que estava lá, como no cadastro.
func opcional(campo, bruto string, maximo int) (*string, error) {
	v := strings.TrimSpace(bruto)
	if v == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(v) > maximo {
		return nil, erro("%s passa de %d caracteres", campo, maximo)
	}
	return &v, nil
}

// Nome do bot, como aparece na tela.
func Nome(bruto string) (string, error) { return texto("o nome", bruto, 1, MaxNome) }

// Slug: identidade técnica do bot. Não se troca depois (trocar perderia o pareamento do
// WhatsApp, que mora numa pasta com este nome) — quem barra a troca é a rota, não esta função.
func Slug(bruto string) (string, error) {
	v := strings.TrimSpace(strings.ToLower(bruto))
	if v == "" {
		return "", erro("o identificador é obrigatório")
	}
	if !reSlug.MatchString(v) {
		return "", erro("identificador inválido: %q (use de 3 a 40 caracteres, só minúscula, número e hífen, começando e terminando com letra ou número)", bruto)
	}
	return v, nil
}

// Instrucoes: quem o bot é, o que pode e o que não pode. Exijo um mínimo de propósito —
// instrução de três palavras produz bot que inventa.
func Instrucoes(bruto string) (string, error) {
	return texto("as instruções", bruto, MinInstrucoes, MaxInstrucoes)
}

// Saudacao é opcional: vazia, o modelo abre a conversa do jeito dele.
func Saudacao(bruto string) (*string, error) { return opcional("a saudação", bruto, MaxSaudacao) }

// Modelo do bot; vazio vira o padrão.
func ModeloDoBot(bruto string) (string, error) {
	v := strings.TrimSpace(bruto)
	if v == "" {
		return ModeloPadrao, nil
	}
	if !ModeloValido(v) {
		nomes := make([]string, 0, len(Modelos))
		for _, m := range Modelos {
			nomes = append(nomes, m.ID)
		}
		return "", erro("modelo desconhecido: %q (use %s)", bruto, strings.Join(nomes, ", "))
	}
	return v, nil
}

// Teto inteiro dentro de um intervalo; zero ou negativo vira o padrão. Os mesmos limites do
// CHECK da migração — a tela ajuda, mas quem recusa é o servidor.
func Teto(campo string, bruto *int, padrao, minimo, maximo int) (int, error) {
	if bruto == nil || *bruto == 0 {
		return padrao, nil
	}
	if *bruto < minimo || *bruto > maximo {
		return 0, erro("%s precisa estar entre %d e %d (recebido %d)", campo, minimo, maximo, *bruto)
	}
	return *bruto, nil
}

// Etapa do fluxo, já validada e pronta para gravar.
type Etapa struct {
	Nome       string
	Objetivo   string
	Instrucoes *string
	Campos     []Campo
}

// Etapas valida o fluxo inteiro: a ordem é a da lista que a tela mandou (a primeira é a etapa 1),
// os ids de campo não se repetem dentro da etapa e todo campo tem rótulo.
//
// Fluxo vazio é permitido de propósito: um bot pode ser só de tirar dúvida, sem coletar nada.
func Etapas(brutas []Etapa) ([]Etapa, error) {
	if len(brutas) > MaxEtapas {
		return nil, erro("o fluxo passa de %d etapas", MaxEtapas)
	}
	saida := make([]Etapa, 0, len(brutas))
	for i, e := range brutas {
		nome, err := texto(fmt.Sprintf("o nome da etapa %d", i+1), e.Nome, 1, MaxNome)
		if err != nil {
			return nil, err
		}
		objetivo, err := texto(fmt.Sprintf("o objetivo da etapa %q", nome), e.Objetivo, MinObjetivo, MaxObjetivo)
		if err != nil {
			return nil, err
		}
		instrucoes, err := opcional(fmt.Sprintf("as instruções da etapa %q", nome), valorOuVazio(e.Instrucoes), MaxInstrucoes)
		if err != nil {
			return nil, err
		}
		campos, err := validarCampos(nome, e.Campos)
		if err != nil {
			return nil, err
		}
		saida = append(saida, Etapa{Nome: nome, Objetivo: objetivo, Instrucoes: instrucoes, Campos: campos})
	}
	return saida, nil
}

func validarCampos(etapa string, brutos []Campo) ([]Campo, error) {
	if len(brutos) > MaxCampos {
		return nil, erro("a etapa %q passa de %d campos", etapa, MaxCampos)
	}
	vistos := make(map[string]bool, len(brutos))
	campos := make([]Campo, 0, len(brutos))
	for _, c := range brutos {
		id := strings.TrimSpace(strings.ToLower(c.ID))
		if !reCampoID.MatchString(id) {
			return nil, erro("campo inválido na etapa %q: %q (use só minúscula, número e _, começando com letra)", etapa, c.ID)
		}
		if vistos[id] {
			return nil, erro("a etapa %q tem o campo %q duas vezes", etapa, id)
		}
		vistos[id] = true
		rotulo, err := texto(fmt.Sprintf("o rótulo do campo %q", id), c.Rotulo, 1, MaxRotulo)
		if err != nil {
			return nil, err
		}
		campos = append(campos, Campo{ID: id, Rotulo: rotulo, Obrigatorio: c.Obrigatorio})
	}
	return campos, nil
}

func valorOuVazio(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
