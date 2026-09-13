package s3src

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// stubS3 serves canned objects. listPages is returned one page per call;
// bodies maps key -> content. getErr, if set, fails GetObject.
type stubS3 struct {
	listPages []*s3.ListObjectsV2Output
	callN     int
	bodies    map[string]string
	getErr    error
	tokensIn  []string
}

func (s *stubS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	s.tokensIn = append(s.tokensIn, aws.ToString(in.ContinuationToken))
	out := s.listPages[s.callN]
	s.callN++
	return out, nil
}

func (s *stubS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	body := s.bodies[aws.ToString(in.Key)]
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(body))}, nil
}

func obj(key string, size int64, mod time.Time) s3types.Object {
	return s3types.Object{Key: aws.String(key), Size: aws.Int64(size), LastModified: aws.Time(mod)}
}

func page(truncated bool, next string, objs ...s3types.Object) *s3.ListObjectsV2Output {
	out := &s3.ListObjectsV2Output{Contents: objs, IsTruncated: aws.Bool(truncated)}
	if next != "" {
		out.NextContinuationToken = aws.String(next)
	}
	return out
}

func TestFetchAllBuildsDocs(t *testing.T) {
	mod := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(false, "", obj("coupa/a.md", 10, mod)),
		},
		bodies: map[string]string{"coupa/a.md": "# Title A\n\nbody"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 1 || docs[0].Key != "coupa/a.md" || !strings.Contains(docs[0].Text, "Title A") {
		t.Fatalf("unexpected docs %+v", docs)
	}
	if !docs[0].LastModified.Equal(mod) {
		t.Fatalf("lost LastModified: %v", docs[0].LastModified)
	}
}

func TestFetchAllPaginates(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(true, "tok", obj("coupa/a.md", 3, mod)),
			page(false, "", obj("coupa/b.md", 3, mod)),
		},
		bodies: map[string]string{"coupa/a.md": "a", "coupa/b.md": "b"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 docs across pages, got %d", len(docs))
	}
	if len(stub.tokensIn) != 2 || stub.tokensIn[1] != "tok" {
		t.Fatalf("want second call to carry continuation token %q, got %v", "tok", stub.tokensIn)
	}
}

func TestFetchAllSkipsEmptyAfterExtract(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(false, "",
				obj("coupa/blank.txt", 4, mod), // non-empty size, extracts to whitespace-only text
				obj("coupa/ok.md", 5, mod),     // good
			),
		},
		bodies: map[string]string{"coupa/blank.txt": "   \n\t", "coupa/ok.md": "# Ok\n\nx"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 1 || docs[0].Key != "coupa/ok.md" {
		t.Fatalf("expected only ok.md, got %+v", docs)
	}
}

func TestFetchAllSkipsUnsupportedAndEmpty(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{
			page(false, "",
				obj("coupa/pic.png", 100, mod), // unsupported ext
				obj("coupa/dir/", 0, mod),      // placeholder
				obj("coupa/empty.md", 0, mod),  // empty body
				obj("coupa/ok.md", 5, mod),     // good
			),
		},
		bodies: map[string]string{"coupa/empty.md": "", "coupa/ok.md": "# Ok\n\nx"},
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	docs, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(docs) != 1 || docs[0].Key != "coupa/ok.md" {
		t.Fatalf("expected only ok.md, got %+v", docs)
	}
}

func TestFetchAllZeroObjectsIsError(t *testing.T) {
	stub := &stubS3{listPages: []*s3.ListObjectsV2Output{page(false, "")}}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	if _, err := c.FetchAll(context.Background()); err == nil {
		t.Fatal("expected error when no ingestible objects found")
	}
}

func TestFetchAllGetError(t *testing.T) {
	mod := time.Now()
	stub := &stubS3{
		listPages: []*s3.ListObjectsV2Output{page(false, "", obj("coupa/a.md", 3, mod))},
		getErr:    io.ErrUnexpectedEOF,
	}
	c := &Client{API: stub, Bucket: "b", Prefix: "coupa/"}
	if _, err := c.FetchAll(context.Background()); err == nil {
		t.Fatal("expected GetObject error to fail the run")
	}
}
