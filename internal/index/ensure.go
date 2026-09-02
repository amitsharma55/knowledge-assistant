package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// EnsureIndex creates the target index with a k-NN + text mapping if it
// doesn't already exist. If the index already exists but its mapping
// predates the team field (added to support per-team filtered search), it
// fails loudly rather than silently leaving stale docs unfilterable: a
// filtered-kNN query against an index with no "team" field mapped returns
// an OpenSearch 400 ("Rewrite first"), and quietly patching the mapping in
// place would leave already-indexed docs without a team value. It fails the
// same way when the existing mapping's vector width doesn't match dim, which
// is what a change of embedding model looks like from here.
//
// Auto deleting and recreating the index is deliberately not done for either
// mismatch — that would silently destroy a production corpus. The operator
// must delete and reindex explicitly.
func (i *Indexer) EnsureIndex(ctx context.Context, dim int) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, i.BaseURL+"/"+i.Index+"/_mapping", nil)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode == 200 {
		var mapping map[string]struct {
			Mappings struct {
				Properties map[string]any `json:"properties"`
			} `json:"mappings"`
		}
		if err := json.Unmarshal(body, &mapping); err != nil {
			return fmt.Errorf("ensure index: decode mapping for %q: %w", i.Index, err)
		}
		for _, idx := range mapping {
			if _, ok := idx.Mappings.Properties["team"]; !ok {
				return fmt.Errorf(
					"ensure index: index %q already exists but its mapping has no %q field; "+
						"filtered-kNN search requires it. Delete the stale index and reindex "+
						"with the fixed mapping (this destroys existing indexed content, so do "+
						"it deliberately): DELETE %s/%s, then re-run the seed/ingest job",
					i.Index, "team", i.BaseURL, i.Index)
			}
			// The vector width is fixed when the index is created, so an
			// index built for a different embedding model can never accept
			// these vectors. Caught here because the alternative is a bulk
			// write that fails per-document with a message naming neither
			// the model nor the setting that chose it. As with the team
			// check, we refuse rather than recreate: dropping the index
			// would silently destroy the corpus.
			if got, ok := mappedDimension(idx.Mappings.Properties); ok && got != dim {
				return fmt.Errorf(
					"ensure index: index %q is mapped for %d-dimension vectors but the "+
						"configured embedder produces %d; they must match. This usually means "+
						"KA_EMBED_MODEL changed. Delete the stale index and reindex (this "+
						"destroys existing indexed content, so do it deliberately): "+
						"DELETE %s/%s, then re-run the seed/ingest job",
					i.Index, got, dim, i.BaseURL, i.Index)
			}
		}
		return nil
	}
	if resp.StatusCode != 404 {
		return fmt.Errorf("ensure index: unexpected status %d checking mapping for %q", resp.StatusCode, i.Index)
	}
	mappingBody := map[string]any{
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
	buf, _ := json.Marshal(mappingBody)
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

// mappedDimension reads the configured width of the knn_vector field from an
// index mapping. It reports false when the field or its dimension is absent
// or malformed, so an unreadable mapping is not treated as a mismatch.
func mappedDimension(props map[string]any) (int, bool) {
	field, ok := props["embedding"].(map[string]any)
	if !ok {
		return 0, false
	}
	// encoding/json decodes numbers into float64 under any.
	d, ok := field["dimension"].(float64)
	if !ok {
		return 0, false
	}
	return int(d), true
}
