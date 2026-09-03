package rag

import "context"

type Chunk struct {
	ID          string `json:"id"`
	Team        string `json:"team"`
	SpaceKey    string `json:"spaceKey"`
	PageID      string `json:"pageId"`
	PageTitle   string `json:"pageTitle"`
	SectionPath string `json:"sectionPath"`
	URL         string `json:"url"`
	Text        string `json:"text"`
	// Score is vector similarity on the (1+cos)/2 scale -- the scale
	// RelevanceFloor is calibrated against.
	Score     float64  `json:"score"`
	ACLGroups []string `json:"aclGroups"`
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
//
// Rank and Reason exist because Score alone is misleading once a reranker is
// in play. Score is vector similarity, computed before the question was ever
// compared to the chunk; the order is the reranker's; and selection is the
// order plus sibling backfill. Showing only the score left the panel saying
// a chunk scored 0.83 and went unused while one at 0.79 was sent, with
// nothing on screen to explain it.
type RetrievedChunk struct {
	Chunk
	Used bool `json:"used"`
	// Rank is the chunk's 1-based position after reranking, which is the
	// order these are sent in.
	Rank int `json:"rank"`
	// Reason is why the chunk did or did not reach the model:
	// "selected" (top-N after reranking), "backfilled" (a sibling section of
	// a page already selected), or "dropped".
	Reason string `json:"reason"`
}

// Turn is one prior message in the conversation. History is what makes a
// follow-up legible: "I am asking about AVR" is a complete answer to the
// assistant's own question and gibberish on its own, and until it was
// threaded through, every turn was answered as though it were the first.
type Turn struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// Rewriter turns a follow-up into a question that stands on its own, for
// retrieval only.
//
// The answering model gets conversation history and can resolve "it" itself.
// Vector search cannot: it embeds the literal string, so "I am asking about
// AVR" retrieves the AVR overview rather than the field mapping the user was
// actually asking after. The rewrite closes that gap, and the user still sees
// their own words -- only the search query changes.
type Rewriter interface {
	Rewrite(ctx context.Context, question string, history []Turn) (string, error)
}

// Reranker reorders retrieved chunks by relevance to the query, judging the
// question and each chunk together -- the signal a bi-encoder vector search
// cannot have, since chunks were embedded before any question existed.
//
// Implementations must return exactly the chunks they were given. A reranker
// is a reordering, never a filter: dropping a chunk here would silently
// shrink the candidate pool on a model's whim.
type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []Chunk) ([]Chunk, error)
}

type LLM interface {
	// Stream sends events to out until the completion ends. Callers close out.
	Stream(ctx context.Context, prompt Prompt, out chan<- StreamEvent) error
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
