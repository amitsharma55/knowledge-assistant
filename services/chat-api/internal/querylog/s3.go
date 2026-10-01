// Package querylog captures answered queries to S3 so the retrieval eval golden
// set can be built from real traffic -- especially the below-floor queries that
// are our true recall failures -- instead of hand-authored guesses.
package querylog

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/example/knowledge-assistant/internal/rag"
)

// putter is the slice of the S3 API this needs; it keeps the sink testable
// without a live bucket.
type putter interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3 writes one JSON object per query under <prefix>/YYYY/MM/DD/. S3 has no
// append, and demo traffic is low, so one object per query is the simple choice.
//
// ponytail: one PutObject per query, fire-and-forget. Batch into daily NDJSON
// objects only if the object count or per-request cost ever bites.
type S3 struct {
	Client putter
	Bucket string
	Prefix string           // e.g. "querylog"; "" means objects sit at the root
	Log    *slog.Logger     // nil -> slog.Default()
	Now    func() time.Time // nil -> time.Now; injectable for tests
}

func (s *S3) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *S3) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Record satisfies rag.QueryLog. It detaches from the caller's context and
// returns immediately: the request context is cancelled the moment the SSE
// stream closes -- which is exactly when this runs -- and a capture miss must
// never delay or fail the answer.
func (s *S3) Record(_ context.Context, rec rag.QueryRecord) {
	key, body := marshal(s.Prefix, s.now(), rec)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(s.Bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(body),
			ContentType: aws.String("application/json"),
		})
		if err != nil {
			s.logger().Warn("querylog: put failed", "err", err, "key", key)
		}
	}()
}

// record is the on-disk shape. Rewritten is omitted when it equals the question,
// so an un-rewritten query doesn't store the string twice.
type record struct {
	TS             time.Time `json:"ts"`
	Team           string    `json:"team"`
	Query          string    `json:"query"`
	Rewritten      string    `json:"rewritten,omitempty"`
	BelowFloor     bool      `json:"belowFloor"`
	SuggestedTeams []string  `json:"suggestedTeams,omitempty"`
}

// marshal builds the object key and body for one record. It is split out so the
// serialization and key layout are unit-testable without S3.
func marshal(prefix string, ts time.Time, rec rag.QueryRecord) (key string, body []byte) {
	ts = ts.UTC()
	r := record{
		TS:             ts,
		Team:           rec.Team,
		Query:          rec.Question,
		BelowFloor:     rec.BelowFloor,
		SuggestedTeams: rec.Suggestions,
	}
	if rec.Rewritten != "" && rec.Rewritten != rec.Question {
		r.Rewritten = rec.Rewritten
	}
	body, _ = json.Marshal(r)
	// UnixNano + a short random suffix keeps keys unique even for two queries
	// in the same nanosecond.
	key = fmt.Sprintf("%s%s/%d-%s.json", prefixSlash(prefix), ts.Format("2006/01/02"), ts.UnixNano(), shortRand())
	return key, body
}

func prefixSlash(p string) string {
	if p == "" {
		return ""
	}
	return p + "/"
}

func shortRand() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
