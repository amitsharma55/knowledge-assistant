package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

type Client struct {
	BaseURL  string
	Index    string
	HTTP     *http.Client
	Embedder rag.Embedder
}

// Search runs k-NN over the embedding field, filtered to the scope's team.
// The scores it returns are on the (1+cos)/2 scale RelevanceFloor expects.
//
// A BM25 half fused by RRF was built and measured here, and removed: it fixed
// literal-token questions (the chunk holding `AVR-TIMEOUT` went from rank 9 to
// rank 1, and stopped flapping) but cost more than it gained on near-identical
// documents. Asked for the Louisiana data call's deadline, seven of the
// query's eight terms match the Texas document's identically titled section
// verbatim, so BM25 promoted it from rank 15 to rank 3 and it reached the
// model. Halving the keyword vote did not fix that -- both rankers retrieve
// the wrong state's section, so it earns votes from both. Corpus check: kNN
// 35/34, hybrid 34/33/33. See git history if revisiting.
func (c *Client) Search(ctx context.Context, query string, scope rag.Scope, k int) ([]rag.Chunk, error) {
	if scope.IsZero() {
		return nil, fmt.Errorf("opensearch: search called with an unscoped request")
	}
	vec, err := c.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	return c.search(ctx, map[string]any{
		"size": k,
		"query": map[string]any{
			"knn": map[string]any{
				"embedding": map[string]any{
					"vector": vec,
					"k":      k,
					"filter": scopeFilter(scope),
				},
			},
		},
	})
}

// search posts one query body and decodes the hits.
func (c *Client) search(ctx context.Context, body map[string]any) ([]rag.Chunk, error) {
	buf, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/%s/_search", c.BaseURL, c.Index)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// Include the response body: OpenSearch explains *why* a search was
		// rejected (a filter clause it cannot rewrite, an unmapped field),
		// and a bare status code turns those into a guessing game.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("opensearch %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Hits struct {
			Hits []struct {
				Source rag.Chunk `json:"_source"`
				Score  float64   `json:"_score"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	chunks := make([]rag.Chunk, 0, len(out.Hits.Hits))
	for _, h := range out.Hits.Hits {
		ch := h.Source
		ch.Score = h.Score
		chunks = append(chunks, ch)
	}
	return chunks, nil
}

// Count reports how many chunks in the scope's team score at or above floor
// against the query, without returning any of them. It backs the no-results
// escape hatch; this path only runs when an answer found nothing, so the
// extra kNN round trip is acceptable. It reuses scopeFilter so the team and
// ACL constraints are identical to Search's.
func (c *Client) Count(ctx context.Context, query string, scope rag.Scope, floor float64) (int, error) {
	if scope.IsZero() {
		return 0, fmt.Errorf("opensearch: count called with an unscoped request")
	}
	hits, err := c.Search(ctx, query, scope, 1000)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hits {
		if h.Score >= floor {
			n++
		}
	}
	return n, nil
}

// scopeFilter constrains a kNN search to the scope's team, and to chunks the
// caller's groups may see. It runs inside the knn clause so that k is applied
// within the team partition rather than across the whole index.
func scopeFilter(scope rag.Scope) map[string]any {
	must := []any{
		map[string]any{"term": map[string]any{"team": scope.Team().Slug()}},
	}
	// The ACL clause is always emitted, even when the caller belongs to zero
	// groups. With an empty groups slice, the "terms" should-clause matches
	// nothing, so minimum_should_match:1 leaves only the "no aclGroups field"
	// branch — i.e. only unrestricted chunks are visible. That matches
	// MemoryStore.aclOK(chunkGroups, userGroups), which denies whenever
	// userGroups is empty and chunkGroups is non-empty.
	// The unrestricted-chunk branch is always present and is the only branch
	// a caller with no groups gets.
	should := []any{
		map[string]any{"bool": map[string]any{
			"must_not": map[string]any{"exists": map[string]any{"field": "aclGroups"}},
		}},
	}
	// The terms clause is emitted only when there is at least one group to
	// match. A "terms" query over an empty array (or a nil slice, which
	// marshals to null) is rejected by OpenSearch inside a kNN filter --
	// the whole search fails with "query must be rewritten first" -- so the
	// clause is omitted rather than emitted empty. Semantics are unchanged:
	// with minimum_should_match:1 an empty terms clause could never have
	// matched anything anyway, leaving the same unrestricted-only view that
	// MemoryStore.aclOK gives a caller with no groups.
	if groups := scope.Groups(); len(groups) > 0 {
		should = append(should, map[string]any{"terms": map[string]any{"aclGroups": groups}})
	}
	must = append(must, map[string]any{"bool": map[string]any{
		"minimum_should_match": 1,
		"should":               should,
	}})
	return map[string]any{"bool": map[string]any{"must": must}}
}
