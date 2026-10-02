package context

// EstimateTokens returns an approximate token count for text.
// Uses len(text)/4, matching the rough average across major LLM tokenizers.
// Deterministic and fast; not exact for any specific model.
func EstimateTokens(text string) int {
	n := len(text) / 4
	if n < 1 && len(text) > 0 {
		return 1
	}
	return n
}
