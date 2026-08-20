// Package anthropic is a minimal streaming client for the Anthropic Messages
// API. It implements rag.LLM. Kept dependency-free so the prototype does not
// require an external SDK; swap in github.com/anthropics/anthropic-sdk-go
// later if we need advanced features (tools, vision, prompt caching).
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/example/knowledge-assistant/internal/rag"
)

const endpoint = "https://api.anthropic.com/v1/messages"

type Client struct {
	APIKey    string
	Model     string
	MaxTokens int
	HTTP      *http.Client
}

type messagesReq struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Stream sends `prompt` as a single user message and forwards Claude's
// content_block_delta text events to `out` as rag.StreamEvent{Type:"token"}.
func (c *Client) Stream(ctx context.Context, prompt string, out chan<- rag.StreamEvent) error {
	maxTok := c.MaxTokens
	if maxTok == 0 {
		maxTok = 1024
	}
	body, _ := json.Marshal(messagesReq{
		Model: c.Model, MaxTokens: maxTok, Stream: true,
		Messages: []message{{Role: "user", Content: prompt}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("anthropic %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var evt struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		if evt.Type != "content_block_delta" || evt.Delta.Type != "text_delta" || evt.Delta.Text == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- rag.StreamEvent{Type: "token", Data: evt.Delta.Text}:
		}
	}
	return scanner.Err()
}
