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

// Counter reports how many chunks at or above floor a query would match in a
// scope, without returning any of them. It backs the no-results escape
// hatch, which tells a multi-team user that their question is answerable
// under another of their teams while leaking nothing but a count. floor is
// the same relevance standard used to decide an answer has no grounding, so
// "not relevant enough to answer with" and "not relevant enough to count"
// stay the same standard.
type Counter interface {
	Count(ctx context.Context, query string, scope Scope, floor float64) (int, error)
}

// TeamSuggestion is a count only. It must never carry chunk text or titles.
type TeamSuggestion struct {
	Team    string `json:"team"`
	Matches int    `json:"matches"`
}
