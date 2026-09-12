package embed

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/example/knowledge-assistant/internal/awsx"
	"github.com/example/knowledge-assistant/internal/rag"
)

// Defaults for the local Ollama embedder. DefaultDim must match the width
// DefaultModel actually returns: the index mapping fixes the vector width at
// creation, so the two move together or not at all.
const (
	DefaultModel     = "nomic-embed-text"
	DefaultDim       = 768
	DefaultOllamaURL = "http://localhost:11434"
)

type Options struct {
	Mode      string // "ollama" | "bedrock" | "mock"
	OllamaURL string
	Model     string
	Dim       int
}

// New builds the embedder and reports the vector width it produces.
//
// Both binaries must call this rather than constructing an embedder
// themselves. The indexer's vectors and the API's query vectors have to come
// from the same model: if they drift apart, every search still returns
// results and every result is meaningless, with no error raised anywhere.
// The dimension comes back alongside the embedder because the index mapping
// needs it, and deriving it here keeps it from being hardcoded at the call
// sites the way it was before.
func New(o Options) (rag.Embedder, int, error) {
	switch o.Mode {
	case "ollama":
		return Ollama{
			BaseURL: o.OllamaURL,
			Model:   o.Model,
			Dim:     o.Dim,
			// Generous: a cold model is loaded into memory on the first
			// call, which takes far longer than a warm embed.
			HTTP: &http.Client{Timeout: 60 * time.Second},
		}, o.Dim, nil
	case "bedrock":
		// Titan on Bedrock is the daily-EKS embedder. The client resolves its
		// region and credentials from the environment (AWS_REGION, Pod Identity).
		client, err := awsx.BedrockRuntime(context.Background())
		if err != nil {
			return nil, 0, fmt.Errorf("embed: %w", err)
		}
		return Bedrock{Model: o.Model, Dim: o.Dim, api: client}, o.Dim, nil
	case "mock":
		return Mock{Dim: o.Dim}, o.Dim, nil
	default:
		return nil, 0, fmt.Errorf(
			"embed: unknown KA_EMBED_MODE %q; want \"ollama\", \"bedrock\" or \"mock\"", o.Mode)
	}
}

// OptionsFromEnv reads the embedder configuration. It lives here, rather than
// in either service's config, so the two binaries cannot be configured
// differently -- see New.
func OptionsFromEnv() Options {
	return Options{
		// Defaults to ollama, not mock. Mock embeddings are hashes with no
		// semantic content, so falling back to them silently yields a
		// confident assistant grounded in unrelated text. Failing to start
		// is the better outcome.
		Mode:      envOr("KA_EMBED_MODE", "ollama"),
		OllamaURL: envOr("KA_OLLAMA_URL", DefaultOllamaURL),
		Model:     envOr("KA_EMBED_MODEL", DefaultModel),
		Dim:       envInt("KA_EMBED_DIM", DefaultDim),
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
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
