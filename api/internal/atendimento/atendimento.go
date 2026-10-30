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
// 1 MB, 500 perguntas) e para a mensagem de erro ser clara, não para apertar quem escreve.
const (
	MaxNome       = 80
	MaxInstrucoes = 20000
	MinInstrucoes = 20
	MaxSaudacao   = 1000
	// Perguntas de um atendimento. Trinta é muito para uma conversa de WhatsApp — quem chegar
	// perto disso tem um formulário, não um atendimento —, mas o limite existe para barrar
	// absurdo, não para dar palpite.
	MaxPerguntas = 30
	MaxPergunta  = 300
	MinPergunta  = 5
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

// Pergunta do atendimento, já validada e pronta para gravar. A ordem é a da lista que a tela
// mandou: o bot faz uma por vez, de cima para baixo (decisão do usuário em 29/10/2026).
//
// `Campo` é onde a resposta é guardada (chave em atendimento.coletas e no lead) e o argumento da
// ferramenta `registrar_resposta`. `Texto` é a pergunta como quem configurou escreveu — o bot
// **não** a repete literalmente: reescreve conforme a conversa.
type Pergunta struct {
	Texto       string  `json:"texto"`
	Campo       string  `json:"campo"`
	Orientacao  *string `json:"orientacao"`
	Obrigatoria bool    `json:"obrigatoria"`
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

// Perguntas valida a lista inteira: a ordem é a da lista que a tela mandou (a primeira é a
// pergunta 1), o campo de cada uma é único no bot e todo campo tem um texto de pergunta.
//
// Lista vazia é permitida de propósito: um bot pode ser só de tirar dúvida, sem perguntar nada.
func Perguntas(brutas []Pergunta) ([]Pergunta, error) {
	if len(brutas) > MaxPerguntas {
		return nil, erro("o atendimento passa de %d perguntas", MaxPerguntas)
	}
	vistos := make(map[string]bool, len(brutas))
	saida := make([]Pergunta, 0, len(brutas))
	for i, q := range brutas {
		texto, err := texto(fmt.Sprintf("a pergunta %d", i+1), q.Texto, MinPergunta, MaxPergunta)
		if err != nil {
			return nil, err
		}
		campo := strings.TrimSpace(strings.ToLower(q.Campo))
		if campo == "" {
			campo = CampoDaPergunta(texto)
		}
		if !reCampoID.MatchString(campo) {
			return nil, erro("o nome do dado da pergunta %d é inválido: %q (use só minúscula, número e _, começando com letra)", i+1, q.Campo)
		}
		if vistos[campo] {
			return nil, erro("duas perguntas guardam a resposta no mesmo dado (%q): a segunda apagaria a primeira", campo)
		}
		vistos[campo] = true
		orientacao, err := opcional(fmt.Sprintf("a orientação da pergunta %d", i+1), valorOuVazio(q.Orientacao), MaxInstrucoes)
		if err != nil {
			return nil, err
		}
		saida = append(saida, Pergunta{Texto: texto, Campo: campo, Orientacao: orientacao, Obrigatoria: q.Obrigatoria})
	}
	return saida, nil
}

// CampoDaPergunta sugere o nome do dado a partir do texto da pergunta: "Quantas vidas vocês
// querem?" → "quantas_vidas_voces_querem". A tela faz o mesmo ao digitar; isto é a rede de
// segurança de quem chama a API direto.
func CampoDaPergunta(pergunta string) string {
	semAcento := strings.NewReplacer(
		"á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i",
		"ó", "o", "õ", "o", "ô", "o", "ú", "u", "ü", "u", "ç", "c",
	).Replace(strings.ToLower(strings.TrimSpace(pergunta)))
	var sb strings.Builder
	for _, r := range semAcento {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case sb.Len() > 0 && !strings.HasSuffix(sb.String(), "_"):
			sb.WriteRune('_')
		}
	}
	campo := strings.Trim(sb.String(), "_")
	// Precisa começar com letra (vira nome de argumento da ferramenta do modelo).
	campo = strings.TrimLeft(campo, "0123456789_")
	if len([]rune(campo)) > 31 {
		campo = string([]rune(campo)[:31])
		campo = strings.Trim(campo, "_")
	}
	return campo
}

func valorOuVazio(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
