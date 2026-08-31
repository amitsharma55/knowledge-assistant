package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// EnsureIndex creates the target index with a k-NN + text mapping if it
// doesn't already exist. Idempotent.
func (i *Indexer) EnsureIndex(ctx context.Context, dim int) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, i.BaseURL+"/"+i.Index, nil)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode == 200 {
		return nil
	}
	body := map[string]any{
		"settings": map[string]any{
			"index": map[string]any{"knn": true},
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				"id":          map[string]any{"type": "keyword"},
				"team":        map[string]any{"type": "keyword"},
				"spaceKey":    map[string]any{"type": "keyword"},
				"pageId":      map[string]any{"type": "keyword"},
				"pageTitle":   map[string]any{"type": "text"},
				"sectionPath": map[string]any{"type": "text"},
				"url":         map[string]any{"type": "keyword"},
				"text":        map[string]any{"type": "text"},
				"updatedAt":   map[string]any{"type": "date"},
				"aclGroups":   map[string]any{"type": "keyword"},
				"embedding": map[string]any{
					"type":      "knn_vector",
					"dimension": dim,
					"method": map[string]any{
						"name":       "hnsw",
						"space_type": "cosinesimil",
						"engine":     "lucene",
					},
				},
			},
		},
	}
	buf, _ := json.Marshal(body)
	req, _ = http.NewRequestWithContext(ctx, http.MethodPut, i.BaseURL+"/"+i.Index, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	resp, err = i.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("create index %d", resp.StatusCode)
	}
	return nil
}
