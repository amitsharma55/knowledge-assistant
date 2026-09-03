// Package rerank reorders retrieved chunks by asking a model to judge them
// against the question.
//
// Retrieval scores a chunk with a bi-encoder: the chunk was compressed into a
// vector at ingest time, before it ever saw the question. That is why generic
// material outranks specific material -- for "what does the middleware return
// when the SOAP call times out", the chunk holding the 504 and AVR-TIMEOUT
// codes placed 9th, behind four chunks about on-call ownership, and never
// reached the model at all. A reranker reads the question and the chunk
// together, which is the signal the vector search structurally cannot have.
package rerank

import "github.com/example/knowledge-assistant/internal/rag"

// Reorder applies a model-produced ranking to chunks, tolerating a bad one.
//
// The ranking arrives as a list of 1-based ids and is not trustworthy: models
// drop ids, repeat them, and invent them. Observed on the first real call --
// gpt-oss returned 19 ids for 20 chunks. A dropped id must not mean a dropped
// chunk, so anything the ranking omits keeps its retrieval order at the back,
// where a chunk the reranker did not rate belongs. Out-of-range and duplicate
// ids are discarded.
//
// The result therefore always holds exactly the input chunks, whatever the
// model said.
func Reorder(chunks []rag.Chunk, order []int) []rag.Chunk {
	out := make([]rag.Chunk, 0, len(chunks))
	taken := make([]bool, len(chunks))
	for _, id := range order {
		i := id - 1
		if i < 0 || i >= len(chunks) || taken[i] {
			continue
		}
		taken[i] = true
		out = append(out, chunks[i])
	}
	for i, c := range chunks {
		if !taken[i] {
			out = append(out, c)
		}
	}
	return out
}
