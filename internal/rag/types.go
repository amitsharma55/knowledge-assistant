package rag

import "context"

type Chunk struct {
	ID          string   `json:"id"`
	Team        string   `json:"team"`
	SpaceKey    string   `json:"spaceKey"`
	PageID      string   `json:"pageId"`
	PageTitle   string   `json:"pageTitle"`
	SectionPath string   `json:"sectionPath"`
	URL         string   `json:"url"`
	Text        string   `json:"text"`
	Score       float64  `json:"score"`
	ACLGroups   []string `json:"aclGroups"`
}

type Retriever interface {
	Search(ctx context.Context, query string, scope Scope, k int) ([]Chunk, error)
}

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// StreamEvent is one token or metadata payload streamed to the client.
type StreamEvent struct {
	Type string `json:"type"` // "retrieval" | "token" | "citation" | "done" | "error"
	Data any    `json:"data"`
}

// RetrievedChunk is a chunk as shown in the UI's context panel. Used reports
// whether it was passed to the model; chunks that were retrieved and then cut
// are the most useful signal when an answer is wrong, so they are sent too.
type RetrievedChunk struct {
	Chunk
	Used bool `json:"used"`
}

type LLM interface {
	// Stream sends events to out until the completion ends. Callers close out.
	Stream(ctx context.Context, prompt string, out chan<- StreamEvent) error
}
