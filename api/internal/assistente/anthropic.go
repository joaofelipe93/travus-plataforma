package assistente

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// ModeloPadrao: o mais capaz para montar consultas certas. Troque com ASSISTENTE_MODELO.
const ModeloPadrao = "claude-opus-5"

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
		// Pensamento adaptativo com esforço médio: pergunta de chat não precisa do máximo, e a
		// resposta começa antes.
		Thinking:     anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
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

func temFallbackNoServidor(nome string) bool {
	return strings.HasPrefix(nome, "claude-opus-5") || strings.HasPrefix(nome, "claude-fable-5")
}
