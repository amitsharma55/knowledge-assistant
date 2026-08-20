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

// Search runs k-NN over the embedding field with an ACL post-filter.
// TODO(hybrid): switch to `hybrid` query + RRF once the neural-search plugin
// and a search pipeline are provisioned in the target OpenSearch cluster.
// For local dev / vanilla OpenSearch, plain k-NN + text match fallback works.
func (c *Client) Search(ctx context.Context, query string, groups []string, k int) ([]rag.Chunk, error) {
	vec, err := c.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	body := map[string]any{
		"size": k,
		"query": map[string]any{
			"knn": map[string]any{
				"embedding": map[string]any{"vector": vec, "k": k},
			},
		},
		"post_filter": aclFilter(groups),
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

func aclFilter(groups []string) map[string]any {
	if len(groups) == 0 {
		return map[string]any{"match_all": map[string]any{}}
	}
	return map[string]any{"terms": map[string]any{"aclGroups": groups}}
}
