package osclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// New must not sign for a local (non-AWS) host: local OpenSearch has no IAM
// auth, and a signed request there is pointless.
func TestNewPlainForLocalhost(t *testing.T) {
	c, err := New(context.Background(), "http://localhost:9200", time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Transport != nil {
		t.Fatalf("expected a plain client (nil Transport) for a localhost host, got %T", c.Transport)
	}
}

// The signing transport must add a SigV4 Authorization header scoped to the
// "es" service and still deliver the original body unchanged.
func TestSigningTransportSignsRequests(t *testing.T) {
	var gotAuth, gotDate, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotDate = r.Header.Get("X-Amz-Date")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret", Source: "test"}, nil
	})
	client := &http.Client{Transport: &signingTransport{
		base:   http.DefaultTransport,
		signer: v4.NewSigner(),
		creds:  creds,
		region: "us-east-1",
	}}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/_bulk", strings.NewReader("payload-bytes"))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("Authorization not SigV4: %q", gotAuth)
	}
	if !strings.Contains(gotAuth, "/es/aws4_request") {
		t.Fatalf("Authorization not scoped to the es service: %q", gotAuth)
	}
	if gotDate == "" {
		t.Fatal("X-Amz-Date header not set")
	}
	if gotBody != "payload-bytes" {
		t.Fatalf("body altered by signing: %q", gotBody)
	}
}
