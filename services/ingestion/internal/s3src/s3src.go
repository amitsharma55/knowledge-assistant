// Package s3src ingests a team's documents from an S3 prefix. Team is never
// inferred from a key: the caller lists exactly one team's prefix and stamps
// the team itself.
package s3src

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/example/knowledge-assistant/internal/extract"
)

// api is the slice of the S3 client this package uses; tests stub it.
type api interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type Client struct {
	API    api
	Bucket string
	Prefix string
	Log    *slog.Logger
}

// Doc is one ingestible object's extracted text.
type Doc struct {
	Key          string
	Text         string
	LastModified time.Time
}

func (c *Client) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

// FetchAll lists the prefix and returns the extracted text of every ingestible
// object. Transport errors and an empty result are returned as errors; a single
// object that fails to extract or is empty is logged and skipped.
func (c *Client) FetchAll(ctx context.Context) ([]Doc, error) {
	var docs []Doc
	var token *string
	for {
		out, err := c.API.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.Bucket),
			Prefix:            aws.String(c.Prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("s3src: list %s/%s: %w", c.Bucket, c.Prefix, err)
		}
		for _, o := range out.Contents {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") || aws.ToInt64(o.Size) == 0 {
				continue // directory placeholder or empty object
			}
			if !extract.Supported(key) {
				continue // not a format we can read
			}
			body, err := c.get(ctx, key)
			if err != nil {
				return nil, err // transport error is fatal
			}
			text, err := extract.FromBytes(key, body)
			if err != nil || strings.TrimSpace(text) == "" {
				c.log().Warn("s3src: skipping unreadable object", "key", key, "err", err)
				continue
			}
			docs = append(docs, Doc{Key: key, Text: text, LastModified: aws.ToTime(o.LastModified)})
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("s3src: no ingestible documents under %s/%s; check the bucket, prefix and credentials", c.Bucket, c.Prefix)
	}
	return docs, nil
}

func (c *Client) get(ctx context.Context, key string) ([]byte, error) {
	out, err := c.API.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("s3src: get %s: %w", key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3src: read %s: %w", key, err)
	}
	return body, nil
}
