package config

import (
	"os"
	"strconv"
	"time"
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
	// MaxContext is a hard ceiling on how many chunks reach the prompt.
	// RerankTopN selects on relevance; sibling backfill may push past it to
	// finish a document, and this stops that from running to the size of the
	// retrieval pool. Left at 0 the pool itself is the only limit, which on
	// a RerankTopN=16 deployment meant 19-chunk prompts.
	MaxContext int
	// RerankMode selects the reranker: "ollama" or "off". Off leaves chunks
	// in vector-search order, which is what RerankTopN cut for as long as no
	// reranker existed.
	RerankMode    string
	RerankURL     string
	RerankModel   string
	RerankTimeout time.Duration
	// RewriteMode selects the follow-up query rewriter: "ollama" or "off".
	// It only ever runs on a turn that has history behind it.
	RewriteMode    string
	RewriteURL     string
	RewriteModel   string
	RewriteTimeout time.Duration
	// RetrieveMode: "single" searches on the rewritten query alone, "dual"
	// searches the question and its rewrite and merges. Dual costs a second
	// search and a bigger rerank prompt, and buys back the chunks a bad
	// rewrite would otherwise lose.
	RetrieveMode string
	// RelevanceFloor is a similarity cutoff on the (1+cos)/2 scale that both
	// retrievers report -- see opensearch.similarity. It gates only the
	// cross-team suggestion: chunks below it are still sent to the model, and
	// the "I don't have that information" refusal comes from the prompt, not
	// from this value.
	//
	// Calibrated against the 36-question set in fixtures/tests.jsonl over the
	// 23-document corpus, using nomic-embed-text:
	//
	//	30 answerable questions      lowest top score  0.8421
	//	6 not-answerable-here        highest top score 0.7739
	//
	// The default is the midpoint of that gap, rounded. The two populations
	// separate cleanly, so any value in (0.7739, 0.8421) is correct for this
	// corpus; the midpoint leaves the most room on both sides.
	//
	// Re-derive it with `python3 scripts/check_corpus.py --scores` after
	// changing KA_EMBED_MODEL or materially changing the corpus. Absolute
	// cosine thresholds do not transfer between embedding models.
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
		RerankTopN:      envInt("KA_RERANK_TOP_N", 6),
		MaxContext:      envInt("KA_MAX_CONTEXT", 10),
		RerankMode:      envOr("KA_RERANK_MODE", "off"),
		RerankURL:       envOr("KA_RERANK_URL", envOr("KA_OLLAMA_URL", "http://localhost:11434")),
		RerankModel:     envOr("KA_RERANK_MODEL", "gpt-oss:20b"),
		// A local 20b takes ~12s to rank 20 chunks from cold, and this sits
		// in front of every answer. The ceiling is generous enough not to
		// trip on a model load, and the orchestrator falls back to
		// retrieval order when it does trip.
		RerankTimeout: envDuration("KA_RERANK_TIMEOUT", 45*time.Second),
		RewriteMode:   envOr("KA_REWRITE_MODE", "off"),
		RewriteURL:    envOr("KA_REWRITE_URL", envOr("KA_RERANK_URL", envOr("KA_OLLAMA_URL", "http://localhost:11434"))),
		RewriteModel:  envOr("KA_REWRITE_MODEL", "gpt-oss:20b"),
		// Rewriting one short question is far less work than ranking twenty
		// chunks, but it sits in front of retrieval, so it gets a tighter
		// ceiling than the reranker's.
		RewriteTimeout: envDuration("KA_REWRITE_TIMEOUT", 20*time.Second),
		RetrieveMode:   envOr("KA_RETRIEVE_MODE", "single"),
		RelevanceFloor: envFloat("KA_RELEVANCE_FLOOR", 0.81),
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

func envDuration(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if t, err := time.ParseDuration(v); err == nil {
			return t
		}
	}
	return d
}
