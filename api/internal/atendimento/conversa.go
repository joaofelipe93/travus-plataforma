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

// EtapaPrompt é a etapa em que a conversa está. Nil quando o bot não tem fluxo (bot só de tirar
// dúvida) ou quando o fluxo acabou.
type EtapaPrompt struct {
	Nome       string
	Objetivo   string
	Instrucoes *string
	Campos     []Campo
	// Ordem na lista e total, para o modelo saber onde está ("etapa 2 de 4").
	Posicao, Total int
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
  equipe responde. Não invente preço, prazo, condição, endereço nem disponibilidade.
- Nunca peça senha, código de verificação, número de cartão, foto de documento nem dado de
  pagamento. Se a pessoa oferecer, peça para ela não mandar.
- Se a pessoa pedir para falar com alguém, ou ficar claramente incomodada, registre o que ela
  precisa e diga que alguém da equipe vai responder. Depois chame a ferramenta de encerrar.
- Se o assunto não é o seu, diga isso com clareza em vez de tentar ajudar por fora do escopo.`

// Prompt monta o bloco fixo do prompt (o que entra no cache): quem o bot é, as regras da casa e
// a etapa atual com os campos que faltam. (O nome não é `Instrucoes` porque isso já é a validação
// do campo de instruções do bot, em atendimento.go.)
func Prompt(b BotPrompt, e *EtapaPrompt, coletado map[string]string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Você é o atendimento de %s no WhatsApp.\n\n%s\n\n%s\n", b.Nome, strings.TrimSpace(b.Instrucoes), regrasFixas)

	if e == nil {
		sb.WriteString("\nEste atendimento não tem etapas configuradas: converse, responda o que souber e " +
			"chame a ferramenta de encerrar quando o assunto terminar.\n")
	} else {
		fmt.Fprintf(&sb, "\nEtapa atual: %s", e.Nome)
		if e.Total > 0 {
			fmt.Fprintf(&sb, " (%d de %d)", e.Posicao, e.Total)
		}
		fmt.Fprintf(&sb, "\nObjetivo desta etapa: %s\n", strings.TrimSpace(e.Objetivo))
		if e.Instrucoes != nil && strings.TrimSpace(*e.Instrucoes) != "" {
			fmt.Fprintf(&sb, "Orientação desta etapa: %s\n", strings.TrimSpace(*e.Instrucoes))
		}
		if len(e.Campos) > 0 {
			sb.WriteString("\nDados desta etapa:\n")
			for _, c := range e.Campos {
				obrig := "opcional"
				if c.Obrigatorio {
					obrig = "obrigatório"
				}
				if v, ok := coletado[c.ID]; ok {
					fmt.Fprintf(&sb, "- %s (%s, %s): já coletado — %q\n", c.ID, c.Rotulo, obrig, v)
				} else {
					fmt.Fprintf(&sb, "- %s (%s, %s): ainda não coletado\n", c.ID, c.Rotulo, obrig)
				}
			}
		}
		sb.WriteString("\nUse `registrar_dado` assim que a pessoa informar um dado — inclusive quando ela contar " +
			"várias coisas de uma vez (uma chamada por dado). Com os obrigatórios desta etapa coletados, chame " +
			"`avancar_etapa`. Não anuncie que está registrando nem que mudou de etapa: isso é assunto interno.\n")
	}

	// O que já foi coletado em etapas anteriores: evita perguntar de novo o que a pessoa já disse.
	if fora := coletadoDeFora(e, coletado); len(fora) > 0 {
		sb.WriteString("\nJá sabemos desta pessoa (não pergunte de novo):\n")
		for _, l := range fora {
			sb.WriteString("- " + l + "\n")
		}
	}
	return sb.String()
}

// coletadoDeFora lista o que já foi coletado e não pertence à etapa atual, em ordem estável
// (mapa em Go não tem ordem, e prompt que muda de ordem a cada resposta estraga o cache).
func coletadoDeFora(e *EtapaPrompt, coletado map[string]string) []string {
	daEtapa := map[string]bool{}
	if e != nil {
		for _, c := range e.Campos {
			daEtapa[c.ID] = true
		}
	}
	linhas := make([]string, 0, len(coletado))
	for campo, valor := range coletado {
		if !daEtapa[campo] {
			linhas = append(linhas, fmt.Sprintf("%s: %q", campo, valor))
		}
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

// CamposPendentes devolve os obrigatórios da etapa que ainda não foram coletados. É o que decide
// se `avancar_etapa` pode valer: quem decide não é o modelo, é esta função.
func CamposPendentes(e *EtapaPrompt, coletado map[string]string) []string {
	if e == nil {
		return nil
	}
	var faltam []string
	for _, c := range e.Campos {
		if !c.Obrigatorio {
			continue
		}
		if v, ok := coletado[c.ID]; !ok || strings.TrimSpace(v) == "" {
			faltam = append(faltam, c.ID)
		}
	}
	return faltam
}
