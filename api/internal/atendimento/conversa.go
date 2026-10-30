package atendimento

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// O prompt de um bot, montado a cada resposta. Estas structs existem para o pacote não precisar
// conhecer o banco: quem carrega as linhas é a API, e aqui só entra texto.

// BotPrompt é o que o bot é.
type BotPrompt struct {
	Nome       string
	Instrucoes string
}

// Regras que valem para todo bot, acima do que o operador escreveu. Elas estão aqui, e não na
// tela, porque não são preferência de quem configura: são o que separa um atendimento de um
// problema. Três grupos:
//
//   - **o canal**: WhatsApp é conversa, não formulário nem documento;
//   - **honestidade**: o bot não finge ser pessoa, não inventa e não promete;
//   - **segurança de quem está do outro lado**: bot nenhum pede senha, código ou cartão. Um
//     atendimento automático é um alvo óbvio de engenharia social, e quem se passaria por nós
//     começaria exatamente assim.
const regrasFixas = `Como você se comporta nesta conversa:

- Você responde por WhatsApp. Escreva curto, no tom de quem conversa, uma ideia por mensagem.
  Nada de markdown, títulos, listas numeradas ou texto de formulário.
- Pergunte uma coisa por vez. Se a pessoa já respondeu algo, não pergunte de novo.
- Você é um atendimento automático, e diz isso se perguntarem. Nunca afirme ser uma pessoa.
- Só afirme o que está nas suas instruções. Se não souber, diga que não sabe e que alguém da
  equipe responde. Não invente preço, prazo, condição, cobertura nem disponibilidade.
- Nunca peça senha, código de verificação, número de cartão, foto de documento nem dado de
  pagamento. Se a pessoa oferecer, peça para ela não mandar.
- Se a pessoa pedir para falar com alguém, ou ficar claramente incomodada, registre o que ela
  precisa e diga que alguém da equipe vai responder. Depois chame a ferramenta de encerrar.
- Se o assunto não é o seu, diga isso com clareza em vez de tentar ajudar por fora do escopo.`

// PerguntaPrompt é uma pergunta do atendimento, com o que já se sabe dela.
type PerguntaPrompt struct {
	Texto       string
	Campo       string
	Orientacao  *string
	Obrigatoria bool
	// Resposta já registrada; vazio quando ainda não foi respondida.
	Resposta string
}

// Respondida diz se esta pergunta já tem resposta.
func (p PerguntaPrompt) Respondida() bool { return strings.TrimSpace(p.Resposta) != "" }

// Prompt monta o bloco fixo do prompt (o que entra no cache): quem o bot é, as regras da casa e
// **a lista de perguntas na ordem**, marcando o que já foi respondido.
//
// A forma mudou em 29/10/2026, a pedido do usuário: era "etapa com objetivo e campos", virou uma
// lista de perguntas. O que o modelo recebe, por isso, é mais direto — ele vê a próxima pergunta
// pendente e o que já foi dito, sem precisar entender o conceito de etapa.
//
// O bot **não repete a pergunta literalmente**: ela é o que se quer saber, não o texto a enviar.
// Quem escreve a frase é o modelo, conforme o que a pessoa acabou de dizer.
func Prompt(b BotPrompt, perguntas []PerguntaPrompt, extras map[string]string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Você é o atendimento de %s no WhatsApp.\n\n%s\n\n%s\n", b.Nome, strings.TrimSpace(b.Instrucoes), regrasFixas)

	if len(perguntas) == 0 {
		sb.WriteString("\nEste atendimento não tem perguntas configuradas: converse, responda o que souber e " +
			"chame a ferramenta de encerrar quando o assunto terminar.\n")
		if fora := extrasEmOrdem(extras); len(fora) > 0 {
			sb.WriteString("\nJá sabemos desta pessoa (não pergunte de novo):\n")
			for _, l := range fora {
				sb.WriteString("- " + l + "\n")
			}
		}
		return sb.String()
	}

	sb.WriteString("\nO que este atendimento precisa descobrir, nesta ordem:\n")
	for i, q := range perguntas {
		obrig := "opcional"
		if q.Obrigatoria {
			obrig = "obrigatória"
		}
		if q.Respondida() {
			fmt.Fprintf(&sb, "%d. %s (%s) — JÁ RESPONDIDA: %q\n", i+1, q.Texto, obrig, q.Resposta)
		} else {
			fmt.Fprintf(&sb, "%d. %s (%s) — ainda sem resposta [dado: %s]\n", i+1, q.Texto, obrig, q.Campo)
		}
		if q.Orientacao != nil && strings.TrimSpace(*q.Orientacao) != "" {
			fmt.Fprintf(&sb, "   Orientação: %s\n", strings.TrimSpace(*q.Orientacao))
		}
	}

	if proxima := Proxima(perguntas); proxima != nil {
		fmt.Fprintf(&sb, "\nAgora trate da pergunta %d: %s\n", proxima.Posicao, proxima.Texto)
		sb.WriteString("Faça-a com as suas palavras, encaixada na conversa — não copie o texto acima e não " +
			"anuncie que está seguindo uma lista. Se a pessoa já tiver dito a resposta sem você perguntar, " +
			"registre e siga para a próxima.\n")
	} else {
		sb.WriteString("\nTodas as perguntas obrigatórias foram respondidas: encerre a conversa com a " +
			"ferramenta de encerrar, depois de agradecer e dizer o que acontece em seguida.\n")
	}
	sb.WriteString("\nUse `registrar_resposta` assim que a pessoa informar algo — inclusive quando ela contar " +
		"várias coisas de uma vez (uma chamada por dado). Não anuncie que está registrando: isso é assunto " +
		"interno.\n")

	// O que foi registrado e não pertence a pergunta nenhuma: o modelo anotou algo a mais, ou a
	// pergunta foi tirada depois. Entra em ordem estável (mapa em Go não tem ordem, e prompt que
	// muda de ordem a cada resposta estraga o cache).
	if fora := extrasEmOrdem(extras); len(fora) > 0 {
		sb.WriteString("\nOutras coisas que já sabemos desta pessoa (não pergunte de novo):\n")
		for _, l := range fora {
			sb.WriteString("- " + l + "\n")
		}
	}
	return sb.String()
}

// ProximaPergunta é a pergunta da vez, com a posição na lista (1 é a primeira).
type ProximaPergunta struct {
	PerguntaPrompt
	Posicao int
}

// Proxima devolve a primeira pergunta sem resposta — **é isto que substitui o controle de
// etapa**: não há estado de "onde a conversa está", porque dá para saber olhando o que já foi
// respondido. Pergunta opcional sem resposta também é oferecida, mas não segura o encerramento
// (ver Pendentes).
func Proxima(perguntas []PerguntaPrompt) *ProximaPergunta {
	for i, q := range perguntas {
		if !q.Respondida() {
			return &ProximaPergunta{PerguntaPrompt: q, Posicao: i + 1}
		}
	}
	return nil
}

// Pendentes lista os campos das perguntas **obrigatórias** ainda sem resposta. É o que diz se o
// atendimento terminou: quem decide não é o modelo, é esta função.
func Pendentes(perguntas []PerguntaPrompt) []string {
	var faltam []string
	for _, q := range perguntas {
		if q.Obrigatoria && !q.Respondida() {
			faltam = append(faltam, q.Campo)
		}
	}
	return faltam
}

func extrasEmOrdem(extras map[string]string) []string {
	linhas := make([]string, 0, len(extras))
	for campo, valor := range extras {
		linhas = append(linhas, fmt.Sprintf("%s: %q", campo, valor))
	}
	sort.Strings(linhas)
	return linhas
}

// Contexto é o bloco que muda a cada resposta (fica fora do cache do prompt).
func Contexto(agora time.Time, contato string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Agora: %s (fuso de Brasília).", agora.Format("02/01/2006 15:04"))
	if c := strings.TrimSpace(contato); c != "" {
		fmt.Fprintf(&sb, "\nO WhatsApp desta pessoa está no nome de %q — pode ser o nome dela ou não; confirme antes de usar.", c)
	}
	return sb.String()
}

// Motivos de encerramento que a ferramenta aceita. São poucos de propósito: é o que a tela
// agrupa depois ("quantos pediram humano?"), não texto livre.
var MotivosEncerramento = []string{"concluido", "fora_do_escopo", "desistiu", "pediu_humano"}

// MotivoValido diz se o motivo que o modelo mandou é um dos conhecidos.
func MotivoValido(m string) bool {
	for _, v := range MotivosEncerramento {
		if v == m {
			return true
		}
	}
	return false
}
