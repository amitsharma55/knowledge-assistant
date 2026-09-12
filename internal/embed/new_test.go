package embed

import (
	"strings"
	"testing"
)

func TestNewOllamaMode(t *testing.T) {
	e, dim, err := New(Options{Mode: "ollama", OllamaURL: "http://localhost:11434", Model: "nomic-embed-text", Dim: 768})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if dim != 768 {
		t.Errorf("dim = %d, want 768", dim)
	}
	o, ok := e.(Ollama)
	if !ok {
		t.Fatalf("got %T, want Ollama", e)
	}
	if o.Model != "nomic-embed-text" || o.Dim != 768 {
		t.Errorf("Ollama = %+v, want model nomic-embed-text and dim 768", o)
	}
	if o.HTTP == nil {
		t.Error("HTTP client is nil; New must set a timeout rather than leave the default")
	}
}

func TestNewMockMode(t *testing.T) {
	e, dim, err := New(Options{Mode: "mock", Dim: 16})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if dim != 16 {
		t.Errorf("dim = %d, want 16", dim)
	}
	m, ok := e.(Mock)
	if !ok {
		t.Fatalf("got %T, want Mock", e)
	}
	if m.Dim != 16 {
		t.Errorf("Mock.Dim = %d, want 16", m.Dim)
	}
}

// An unrecognised mode must not quietly fall back to Mock: hash vectors
// produce fluent answers over unrelated chunks, which fails invisibly.
func TestNewRejectsUnknownMode(t *testing.T) {
	_, _, err := New(Options{Mode: "titan", Dim: 768})
	if err == nil {
		t.Fatal("want error for unknown mode, got nil")
	}
	if !strings.Contains(err.Error(), "titan") {
		t.Errorf("error %q does not name the offending mode", err)
	}
}

func TestOptionsFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"KA_EMBED_MODE", "KA_OLLAMA_URL", "KA_EMBED_MODEL", "KA_EMBED_DIM"} {
		t.Setenv(k, "")
	}
	o := OptionsFromEnv()
	// Defaulting to ollama rather than mock is deliberate: a silent fall
	// back to hash vectors is the exact failure this package exists to end.
	if o.Mode != "ollama" {
		t.Errorf("Mode = %q, want ollama", o.Mode)
	}
	if o.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", o.Model, DefaultModel)
	}
	if o.Dim != DefaultDim {
		t.Errorf("Dim = %d, want %d", o.Dim, DefaultDim)
	}
	if o.OllamaURL == "" {
		t.Error("OllamaURL is empty; a default is required")
	}
}

func TestOptionsFromEnvOverrides(t *testing.T) {
	t.Setenv("KA_EMBED_MODE", "mock")
	t.Setenv("KA_OLLAMA_URL", "http://ollama:11434")
	t.Setenv("KA_EMBED_MODEL", "bge-m3")
	t.Setenv("KA_EMBED_DIM", "1024")

	o := OptionsFromEnv()
	if o.Mode != "mock" || o.OllamaURL != "http://ollama:11434" || o.Model != "bge-m3" || o.Dim != 1024 {
		t.Errorf("OptionsFromEnv() = %+v, want the four env overrides applied", o)
	}
}

func TestNewBedrock(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	e, dim, err := New(Options{Mode: "bedrock", Model: "amazon.titan-embed-text-v2:0", Dim: 1024})
	if err != nil {
		t.Fatalf("New bedrock: %v", err)
	}
	if dim != 1024 {
		t.Fatalf("dim = %d, want 1024", dim)
	}
	if _, ok := e.(Bedrock); !ok {
		t.Fatalf("got %T, want Bedrock", e)
	}
}
