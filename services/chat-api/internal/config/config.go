package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr            string
	FixturesDir     string // if set, preload MemoryStore from *.md files and skip OpenSearch
	OpenSearchURL   string
	OpenSearchIdx   string
	PostgresDSN     string // empty disables persistent chats
	LLMMode         string // "mock" | "anthropic" | "bedrock"
	AnthropicAPIKey string
	AnthropicModel  string
	BedrockRegion   string
	BedrockModelID  string
	// Embedder settings are deliberately absent: they are read by
	// embed.OptionsFromEnv so that chat-api and the indexer cannot be
	// configured onto different models. See internal/embed/new.go.
	TopK       int
	RerankTopN int
	// RelevanceFloor is a similarity cutoff on the (1+cos)/2 scale that both
	// retrievers now report -- see opensearch.similarity. It gates only the
	// cross-team suggestion: chunks below it are still sent to the model,
	// and the "I don't have that information" refusal comes from the prompt,
	// not from this value.
	//
	// The default is empirical and model-specific. Measured against the
	// fixtures corpus with nomic-embed-text, answerable questions score
	// 0.82-0.94 and off-corpus questions still score 0.72-0.74 (embeddings
	// are anisotropic, so unrelated text is nowhere near 0.5). 0.78 sits in
	// that gap. Changing KA_EMBED_MODEL invalidates it. It is calibrated
	// from two data points over a small corpus and should be re-derived
	// against the evaluation set once one exists.
	RelevanceFloor float64
}

func Load() Config {
	return Config{
		Addr:            envOr("KA_ADDR", ":8080"),
		FixturesDir:     os.Getenv("KA_FIXTURES_DIR"),
		OpenSearchURL:   envOr("KA_OPENSEARCH_URL", "http://localhost:9200"),
		OpenSearchIdx:   envOr("KA_OPENSEARCH_INDEX", "kb-chunks"),
		PostgresDSN:     envOr("KA_POSTGRES_DSN", "postgres://ka:ka@localhost:5432/ka?sslmode=disable"),
		LLMMode:         envOr("KA_LLM_MODE", "mock"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:  envOr("KA_ANTHROPIC_MODEL", "claude-sonnet-4-5-20250929"),
		BedrockRegion:   envOr("AWS_REGION", "us-east-1"),
		BedrockModelID:  envOr("KA_BEDROCK_MODEL", "anthropic.claude-sonnet-4-6-v1:0"),
		TopK:            envInt("KA_TOP_K", 8),
		RerankTopN:      envInt("KA_RERANK_TOP_N", 4),
		RelevanceFloor:  envFloat("KA_RELEVANCE_FLOOR", 0.78),
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envFloat(k string, d float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return d
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}
