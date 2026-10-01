package querylog

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/example/knowledge-assistant/internal/rag"
)

func TestMarshal(t *testing.T) {
	ts := time.Date(2026, 9, 30, 14, 5, 6, 0, time.UTC)
	t.Run("full record", func(t *testing.T) {
		key, body := marshal("querylog", ts, rag.QueryRecord{
			Team: "star", Question: "it", Rewritten: "the texas data call deadline",
			BelowFloor: true, Suggestions: []string{"coupa"},
		})
		if !strings.HasPrefix(key, "querylog/2026/09/30/") || !strings.HasSuffix(key, ".json") {
			t.Fatalf("key = %q, want querylog/2026/09/30/<ts>-<rand>.json", key)
		}
		var r record
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatal(err)
		}
		if r.Team != "star" || r.Query != "it" || r.Rewritten != "the texas data call deadline" || !r.BelowFloor {
			t.Fatalf("decoded = %+v", r)
		}
		if len(r.SuggestedTeams) != 1 || r.SuggestedTeams[0] != "coupa" {
			t.Fatalf("suggestions = %v", r.SuggestedTeams)
		}
	})
	t.Run("rewritten omitted when equal to query", func(t *testing.T) {
		_, body := marshal("", ts, rag.QueryRecord{Team: "hr", Question: "same", Rewritten: "same"})
		if strings.Contains(string(body), "rewritten") {
			t.Fatalf("rewritten should be omitted when identical: %s", body)
		}
	})
	t.Run("empty prefix puts at root", func(t *testing.T) {
		key, _ := marshal("", ts, rag.QueryRecord{})
		if !strings.HasPrefix(key, "2026/09/30/") {
			t.Fatalf("key = %q, want no leading slash", key)
		}
	})
}

// capturingPutter records the last key it was asked to put.
type capturingPutter struct {
	mu   sync.Mutex
	key  string
	body string
	done chan struct{}
}

func (c *capturingPutter) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key = *in.Key
	b, _ := io.ReadAll(in.Body)
	c.body = string(b)
	close(c.done)
	return &s3.PutObjectOutput{}, nil
}

func TestRecordPutsObject(t *testing.T) {
	p := &capturingPutter{done: make(chan struct{})}
	s := &S3{Client: p, Bucket: "b", Prefix: "querylog", Now: func() time.Time {
		return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	}}
	s.Record(context.Background(), rag.QueryRecord{Team: "star", Question: "q", BelowFloor: true})
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("PutObject was not called")
	}
	if !strings.HasPrefix(p.key, "querylog/2026/09/30/") {
		t.Fatalf("key = %q", p.key)
	}
	if !strings.Contains(p.body, `"belowFloor":true`) {
		t.Fatalf("body = %s", p.body)
	}
}
