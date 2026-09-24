package assistente

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// ModeloPadrao: o Haiku, rápido e barato (decisão do usuário, 24/09/2026; o 3.5 pedido foi
// aposentado pela Anthropic). Troque com ASSISTENTE_MODELO (ex.: claude-sonnet-5 ou claude-opus-5
// para perguntas mais difíceis).
const ModeloPadrao = "claude-haiku-4-5"

// ModeloAnthropic chama a API do Claude em streaming.
type ModeloAnthropic struct {
	cliente anthropic.Client
	nome    string
}

func NovoModeloAnthropic(chave, nome string) *ModeloAnthropic {
	if nome == "" {
		nome = ModeloPadrao
	}
	return &ModeloAnthropic{cliente: anthropic.NewClient(option.WithAPIKey(chave)), nome: nome}
}

func (m *ModeloAnthropic) Nome() string { return m.nome }

func (m *ModeloAnthropic) Gerar(ctx context.Context, p Pedido, aoTexto func(string)) (*anthropic.BetaMessage, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(m.nome),
		MaxTokens: 16_000,
		System:    p.Sistema,
		Tools:     p.Ferramentas,
		Messages:  p.Mensagens,
	}
	// Pensamento adaptativo com esforço médio só nos modelos que aceitam: o Haiku 4.5 recusa os
	// dois com 400 e roda sem pensamento. Pergunta de chat não precisa do máximo.
	if temPensamentoAdaptativo(m.nome) {
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}}
		params.OutputConfig = anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium}
	}
	// Se o classificador de segurança recusar por engano, o próprio servidor tenta outro modelo
	// (Claude Opus 5 e Fable).
	if temFallbackNoServidor(m.nome) {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}

	stream := m.cliente.Beta.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	msg := &anthropic.BetaMessage{}
	for stream.Next() {
		evento := stream.Current()
		if err := msg.Accumulate(evento); err != nil {
			return msg, err
		}
		if delta, ok := evento.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
			if texto, ok := delta.Delta.AsAny().(anthropic.BetaTextDelta); ok {
				aoTexto(texto.Text)
			}
		}
	}
	return msg, stream.Err()
}

var comPensamentoAdaptativo = []string{
	"claude-opus-5", "claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8",
	"claude-sonnet-5", "claude-sonnet-4-6", "claude-fable-", "claude-mythos-",
}

func temPensamentoAdaptativo(nome string) bool {
	for _, prefixo := range comPensamentoAdaptativo {
		if strings.HasPrefix(nome, prefixo) {
			return true
		}
	}
	return false
}

func temFallbackNoServidor(nome string) bool {
	return strings.HasPrefix(nome, "claude-opus-5") || strings.HasPrefix(nome, "claude-fable-5")
}
