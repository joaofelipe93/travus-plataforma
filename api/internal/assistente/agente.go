// Package assistente é o agente da tela inicial: responde perguntas sobre os serviços da
// plataforma (Canopus, reservas, WhatsApp, Drive…) usando o Claude com ferramentas que só LEEM.
// Nenhuma ferramenta cria execução, aprova lance, manda mensagem ou envia arquivo: o agente não
// tem como agir, só consultar.
//
// O laço é o de tool use: o modelo responde (em streaming, o texto vai direto para a tela); se
// pedir ferramentas, elas rodam aqui e o resultado volta para ele, até a resposta final ou o
// limite de rodadas.
package assistente

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	// Rodadas de ferramenta por pergunta: sobra para "consulta, erra o SQL, corrige, soma".
	MaximoRodadas = 10
	// Tempo de uma ferramenta (o SELECT já tem 5 s no papel do banco).
	tempoFerramenta = 20 * time.Second
	// Resultado de ferramenta maior que isto é cortado: custa tokens e o modelo não precisa.
	maximoResultado = 24_000

	// Histórico mandado pela tela: a conversa fica só no navegador.
	MaximoMensagens      = 30
	MaximoTextoPergunta  = 2_000
	MaximoTextoResposta  = 16_000
	PapelUsuario         = "usuario"
	PapelAssistente      = "assistente"
	textoRecusa          = "Não consigo responder a essa pergunta."
	textoLimiteRodadas   = "Parei aqui: a pergunta pediu consultas demais de uma vez. Tente dividir em perguntas menores."
	textoRespostaCortada = "(A resposta ficou longa demais e foi cortada.)"
)

// Mensagem é uma fala da conversa, como a tela manda.
type Mensagem struct {
	Papel string `json:"papel"` // usuario | assistente
	Texto string `json:"texto"`
}

// Ferramenta que o agente pode chamar. Executar recebe o JSON que o modelo mandou e devolve o
// texto do resultado (de preferência JSON); erro vira tool_result com is_error, e o modelo vê a
// mensagem (não ponha segredo nela).
type Ferramenta struct {
	Nome      string
	Descricao string
	// Propriedades do JSON Schema de entrada e as obrigatórias.
	Parametros  map[string]any
	Obrigatorio []string
	// O que a tela mostra enquanto roda (ex.: "Consultando o banco de dados").
	Rotulo   string
	Executar func(ctx context.Context, entrada json.RawMessage) (string, error)
}

// Evento para a tela.
type Evento struct {
	Tipo       string `json:"tipo"` // texto | ferramenta
	Texto      string `json:"texto,omitempty"`
	Ferramenta string `json:"ferramenta,omitempty"`
	Rotulo     string `json:"rotulo,omitempty"`
}

// Uso soma o que a pergunta gastou (vai para a auditoria).
type Uso struct {
	Rodadas            int      `json:"rodadas"`
	Ferramentas        []string `json:"ferramentas,omitempty"`
	TokensEntrada      int64    `json:"tokens_entrada"`
	TokensSaida        int64    `json:"tokens_saida"`
	TokensCacheLidos   int64    `json:"tokens_cache_lidos"`
	TokensCacheGravado int64    `json:"tokens_cache_gravados"`
	Parada             string   `json:"parada"`
}

// Pedido de uma rodada ao modelo.
type Pedido struct {
	Sistema     []anthropic.BetaTextBlockParam
	Ferramentas []anthropic.BetaToolUnionParam
	Mensagens   []anthropic.BetaMessageParam
}

// Modelo gera uma rodada, chamando aoTexto a cada pedaço de texto que chega. A implementação de
// verdade é ModeloAnthropic; os testes usam um dublê.
type Modelo interface {
	Gerar(ctx context.Context, p Pedido, aoTexto func(string)) (*anthropic.BetaMessage, error)
}

// ErrHistoricoInvalido: a tela mandou uma conversa fora do formato.
var ErrHistoricoInvalido = errors.New("conversa inválida")

// ValidarHistorico confere o que a tela mandou: alterna usuário e assistente, começa e termina
// no usuário, textos dentro do limite.
func ValidarHistorico(msgs []Mensagem) error {
	if len(msgs) == 0 {
		return fmt.Errorf("%w: mande ao menos uma pergunta", ErrHistoricoInvalido)
	}
	if len(msgs) > MaximoMensagens {
		return fmt.Errorf("%w: conversa longa demais (máximo %d mensagens); comece uma nova", ErrHistoricoInvalido, MaximoMensagens)
	}
	for i, m := range msgs {
		esperado := PapelUsuario
		limite := MaximoTextoPergunta
		if i%2 == 1 {
			esperado, limite = PapelAssistente, MaximoTextoResposta
		}
		if m.Papel != esperado {
			return fmt.Errorf("%w: a mensagem %d deveria ser de %q", ErrHistoricoInvalido, i+1, esperado)
		}
		if strings.TrimSpace(m.Texto) == "" && m.Papel == PapelUsuario {
			return fmt.Errorf("%w: pergunta vazia", ErrHistoricoInvalido)
		}
		if len([]rune(m.Texto)) > limite {
			return fmt.Errorf("%w: a mensagem %d passa de %d caracteres", ErrHistoricoInvalido, i+1, limite)
		}
	}
	if len(msgs)%2 == 0 {
		return fmt.Errorf("%w: a última mensagem precisa ser a pergunta", ErrHistoricoInvalido)
	}
	return nil
}

// Agente junta modelo, instruções e ferramentas de uma conversa.
type Agente struct {
	Modelo Modelo
	// Instruções fixas (vão para o cache do prompt).
	Instrucoes string
	// Contexto que muda a cada pergunta (data e hora, quem pergunta): depois do cache.
	Contexto    string
	Ferramentas []Ferramenta
}

// Responder roda o laço de tool use sobre o histórico (já validado) e manda os eventos para a
// tela. Devolve o uso mesmo quando dá erro no meio.
func (a *Agente) Responder(ctx context.Context, historico []Mensagem, emitir func(Evento)) (Uso, error) {
	uso := Uso{}
	pedido := Pedido{
		Sistema: []anthropic.BetaTextBlockParam{
			{Text: a.Instrucoes, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()},
			{Text: a.Contexto},
		},
		Ferramentas: a.definicoes(),
		Mensagens:   converterHistorico(historico),
	}
	porNome := make(map[string]Ferramenta, len(a.Ferramentas))
	for _, f := range a.Ferramentas {
		porNome[f.Nome] = f
	}

	// Texto de rodadas diferentes vira parágrafos diferentes na tela.
	escreveu := false
	novaRodada := false
	aoTexto := func(t string) {
		if t == "" {
			return
		}
		if novaRodada && escreveu {
			t = "\n\n" + t
		}
		novaRodada = false
		escreveu = true
		emitir(Evento{Tipo: "texto", Texto: t})
	}

	for rodada := 0; rodada < MaximoRodadas; rodada++ {
		novaRodada = true
		resp, err := a.Modelo.Gerar(ctx, pedido, aoTexto)
		uso.Rodadas++
		if resp != nil {
			somarUso(&uso, resp)
		}
		if err != nil {
			return uso, err
		}
		uso.Parada = string(resp.StopReason)

		switch resp.StopReason {
		case anthropic.BetaStopReasonToolUse:
		case anthropic.BetaStopReasonRefusal:
			// O que já foi para a tela fica (a tela não tem como apagar), mas avisa.
			aoTexto(textoRecusa)
			return uso, nil
		case anthropic.BetaStopReasonMaxTokens:
			aoTexto(textoRespostaCortada)
			return uso, nil
		default:
			return uso, nil
		}

		pedido.Mensagens = append(pedido.Mensagens, paraHistorico(resp))
		var resultados []anthropic.BetaContentBlockParamUnion
		for _, bloco := range resp.Content {
			chamada, ok := bloco.AsAny().(anthropic.BetaToolUseBlock)
			if !ok {
				continue
			}
			uso.Ferramentas = append(uso.Ferramentas, chamada.Name)
			f, existe := porNome[chamada.Name]
			if !existe {
				resultados = append(resultados, anthropic.NewBetaToolResultBlock(chamada.ID, "ferramenta desconhecida: "+chamada.Name, true))
				continue
			}
			emitir(Evento{Tipo: "ferramenta", Ferramenta: f.Nome, Rotulo: f.Rotulo})
			texto, erro := executar(ctx, f, json.RawMessage(chamada.JSON.Input.Raw()))
			resultados = append(resultados, anthropic.NewBetaToolResultBlock(chamada.ID, texto, erro))
		}
		if len(resultados) == 0 {
			// tool_use sem bloco de ferramenta (não deveria acontecer): encerra em vez de girar.
			return uso, nil
		}
		// Todos os resultados numa mensagem só (o modelo continua pedindo em paralelo).
		pedido.Mensagens = append(pedido.Mensagens, anthropic.NewBetaUserMessage(resultados...))
	}
	aoTexto(textoLimiteRodadas)
	uso.Parada = "limite_rodadas"
	return uso, nil
}

func executar(ctx context.Context, f Ferramenta, entrada json.RawMessage) (texto string, erro bool) {
	ctx, cancel := context.WithTimeout(ctx, tempoFerramenta)
	defer cancel()
	defer func() {
		if v := recover(); v != nil {
			slog.Error("assistente: pânico na ferramenta", "ferramenta", f.Nome, "valor", v)
			texto, erro = "erro interno na ferramenta", true
		}
	}()
	if !json.Valid(entrada) {
		return "entrada inválida: mande um objeto JSON", true
	}
	saida, err := f.Executar(ctx, entrada)
	if err != nil {
		return cortar(err.Error()), true
	}
	return cortar(saida), false
}

func cortar(s string) string {
	if len(s) <= maximoResultado {
		return s
	}
	// Corta em fronteira de rune.
	corte := maximoResultado
	for corte > 0 && !utf8Inicio(s[corte]) {
		corte--
	}
	return s[:corte] + "\n…(resultado cortado: refine a consulta, use agregações ou LIMIT menor)"
}

func utf8Inicio(b byte) bool { return b&0xC0 != 0x80 }

func (a *Agente) definicoes() []anthropic.BetaToolUnionParam {
	defs := make([]anthropic.BetaToolUnionParam, 0, len(a.Ferramentas))
	for _, f := range a.Ferramentas {
		props := f.Parametros
		if props == nil {
			props = map[string]any{}
		}
		obrig := f.Obrigatorio
		if obrig == nil {
			obrig = []string{}
		}
		defs = append(defs, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
			Name:        f.Nome,
			Description: anthropic.String(f.Descricao),
			InputSchema: anthropic.BetaToolInputSchemaParam{
				Properties:  props,
				Required:    obrig,
				ExtraFields: map[string]any{"additionalProperties": false},
			},
		}})
	}
	return defs
}

func converterHistorico(msgs []Mensagem) []anthropic.BetaMessageParam {
	out := make([]anthropic.BetaMessageParam, 0, len(msgs))
	for _, m := range msgs {
		texto := m.Texto
		if m.Papel == PapelAssistente {
			if strings.TrimSpace(texto) == "" {
				texto = "(sem resposta)"
			}
			out = append(out, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(texto)},
			})
			continue
		}
		out = append(out, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(texto)))
	}
	return out
}

// paraHistorico devolve a resposta ao histórico. Depois de um fallback no meio da resposta
// (recusa de um modelo, outro continua), os blocos de pensamento e de ferramenta de antes da
// última troca não podem voltar: só o texto.
func paraHistorico(resp *anthropic.BetaMessage) anthropic.BetaMessageParam {
	ultimaTroca := -1
	for i, b := range resp.Content {
		if b.Type == "fallback" {
			ultimaTroca = i
		}
	}
	if ultimaTroca < 0 {
		return resp.ToParam()
	}
	copia := *resp
	copia.Content = nil
	for i, b := range resp.Content {
		if i < ultimaTroca && b.Type != "text" {
			continue
		}
		if b.Type == "fallback" {
			continue
		}
		copia.Content = append(copia.Content, b)
	}
	return copia.ToParam()
}

func somarUso(uso *Uso, resp *anthropic.BetaMessage) {
	uso.TokensEntrada += resp.Usage.InputTokens
	uso.TokensSaida += resp.Usage.OutputTokens
	uso.TokensCacheLidos += resp.Usage.CacheReadInputTokens
	uso.TokensCacheGravado += resp.Usage.CacheCreationInputTokens
}
