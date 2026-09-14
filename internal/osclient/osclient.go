// Package osclient builds the HTTP client the indexer and chat-api use to talk
// to OpenSearch. Against an AWS managed domain the requests must be SigV4-signed
// or the domain's IAM access policy rejects them with 403; against a local
// OpenSearch (dev) they must not be signed. One constructor decides which by the
// host, so both binaries reach OpenSearch the same way.
package osclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
)

// New returns an HTTP client for OpenSearch at baseURL. For an AWS managed
// domain (an *.amazonaws.com host) every request is SigV4-signed for the "es"
// service with the ambient AWS credentials (Pod Identity in the cluster); the
// domain's IAM policy 403s anything unsigned. For any other host (local dev's
// http://localhost:9200) it returns a plain client, since local OpenSearch has
// no IAM auth.
func New(ctx context.Context, baseURL string, timeout time.Duration) (*http.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("osclient: parse %q: %w", baseURL, err)
	}
	if !strings.HasSuffix(strings.ToLower(u.Hostname()), ".amazonaws.com") {
		return &http.Client{Timeout: timeout}, nil
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("osclient: load aws config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("osclient: AWS region is empty; set AWS_REGION so OpenSearch requests can be SigV4-signed")
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &signingTransport{
			base:   http.DefaultTransport,
			signer: v4.NewSigner(),
			creds:  cfg.Credentials,
			region: cfg.Region,
		},
	}, nil
}

// signingTransport SigV4-signs each request for the "es" service before sending.
type signingTransport struct {
	base   http.RoundTripper
	signer *v4.Signer
	creds  aws.CredentialsProvider
	region string
}

func (t *signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// SigV4 signs over a SHA-256 of the body, so read it, hash it, and hand the
	// transport a fresh reader over the same bytes.
	var payload []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("osclient: read request body: %w", err)
		}
		payload = b
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.ContentLength = int64(len(b))
	}
	sum := sha256.Sum256(payload) // nil payload hashes to the empty-body digest

	creds, err := t.creds.Retrieve(req.Context())
	if err != nil {
		return nil, fmt.Errorf("osclient: retrieve aws credentials: %w", err)
	}
	if err := t.signer.SignHTTP(req.Context(), creds, req, hex.EncodeToString(sum[:]), "es", t.region, time.Now()); err != nil {
		return nil, fmt.Errorf("osclient: sign request: %w", err)
	}
	return t.base.RoundTrip(req)
}
