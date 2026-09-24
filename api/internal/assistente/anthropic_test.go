package assistente

import "testing"

// O Haiku 4.5 recusa pensamento adaptativo, effort e fallbacks com 400: só os modelos que aceitam
// recebem essas opções.
func TestOpcoesPorModelo(t *testing.T) {
	for nome, quer := range map[string][2]bool{
		"claude-haiku-4-5": {false, false},
		"claude-sonnet-5":  {true, false},
		"claude-opus-5":    {true, true},
		"claude-opus-4-5":  {false, false},
		"claude-fable-5-1": {true, true},
	} {
		if got := [2]bool{temPensamentoAdaptativo(nome), temFallbackNoServidor(nome)}; got != quer {
			t.Errorf("%s: pensamento adaptativo e fallback = %v, quer %v", nome, got, quer)
		}
	}
	if ModeloPadrao != "claude-haiku-4-5" {
		t.Errorf("modelo padrão: %s", ModeloPadrao)
	}
}
