package classify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaClassifyParsesBool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": `{"ticket": true}`},
		})
	}))
	defer srv.Close()
	o := Ollama{BaseURL: srv.URL, Model: "gpt-oss:20b", HTTP: srv.Client()}
	yes, err := o.Classify(context.Background(), "any open tickets for star?")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !yes {
		t.Error("Classify = false, want true")
	}
}

func TestParseTicketBool(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{`{"ticket": true}`, true},
		{`sure: {"ticket": false} done`, false},
	} {
		got, err := parseTicketBool(tc.in)
		if err != nil {
			t.Fatalf("parseTicketBool(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseTicketBool(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseTicketBool("no json here"); err == nil {
		t.Error("parseTicketBool on junk = nil error, want error")
	}
}
