package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Doc mirrors the shape indexed in OpenSearch. Keep in sync with the query
// side in services/chat-api/internal/opensearch/client.go.
type Doc struct {
	ID          string    `json:"id"`
	SpaceKey    string    `json:"spaceKey"`
	PageID      string    `json:"pageId"`
	PageTitle   string    `json:"pageTitle"`
	SectionPath string    `json:"sectionPath"`
	URL         string    `json:"url"`
	Text        string    `json:"text"`
	Embedding   []float32 `json:"embedding"`
	UpdatedAt   string    `json:"updatedAt"`
	ACLGroups   []string  `json:"aclGroups"`
}

type Indexer struct {
	BaseURL string
	Index   string
	HTTP    *http.Client
}

// Bulk indexes docs using the _bulk API. Callers should batch (~500 per call).
func (i *Indexer) Bulk(ctx context.Context, docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, d := range docs {
		meta := map[string]any{"index": map[string]any{"_index": i.Index, "_id": d.ID}}
		mb, _ := json.Marshal(meta)
		db, _ := json.Marshal(d)
		buf.Write(mb)
		buf.WriteByte('\n')
		buf.Write(db)
		buf.WriteByte('\n')
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, i.BaseURL+"/_bulk", &buf)
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bulk %d", resp.StatusCode)
	}
	return nil
}
