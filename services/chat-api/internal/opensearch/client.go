package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/example/knowledge-assistant/internal/rag"
)

type Client struct {
	BaseURL  string
	Index    string
	HTTP     *http.Client
	Embedder rag.Embedder
}

// Search runs k-NN over the embedding field, filtered to the scope's team.
// TODO(hybrid): switch to `hybrid` query + RRF once the neural-search plugin
// and a search pipeline are provisioned in the target OpenSearch cluster.
// For local dev / vanilla OpenSearch, plain k-NN + text match fallback works.
func (c *Client) Search(ctx context.Context, query string, scope rag.Scope, k int) ([]rag.Chunk, error) {
	if scope.IsZero() {
		return nil, fmt.Errorf("opensearch: search called with an unscoped request")
	}
	vec, err := c.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	body := map[string]any{
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
	}
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
		return nil, fmt.Errorf("opensearch %d", resp.StatusCode)
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
		c := h.Source
		c.Score = h.Score
		chunks = append(chunks, c)
	}
	return chunks, nil
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
	groups := scope.Groups()
	must = append(must, map[string]any{"bool": map[string]any{
		"minimum_should_match": 1,
		"should": []any{
			map[string]any{"terms": map[string]any{"aclGroups": groups}},
			map[string]any{"bool": map[string]any{
				"must_not": map[string]any{"exists": map[string]any{"field": "aclGroups"}},
			}},
		},
	}})
	return map[string]any{"bool": map[string]any{"must": must}}
}
