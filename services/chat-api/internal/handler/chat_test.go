package handler

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/example/knowledge-assistant/services/chat-api/internal/repo"
)

func TestHistoryTurnsKeepsOnlyTheLastWindow(t *testing.T) {
	var msgs []repo.Message
	for i := 0; i < maxHistoryTurns+4; i++ {
		msgs = append(msgs, repo.Message{Role: "user", Content: "m"})
	}
	got := historyTurns(msgs)
	if len(got) != maxHistoryTurns {
		t.Fatalf("want %d turns after windowing, got %d", maxHistoryTurns, len(got))
	}
}

func TestHistoryTurnsCapsAHugeMessage(t *testing.T) {
	// A single pasted document within the window must not reach the model
	// verbatim: the turn window bounds the number of messages replayed, not
	// the size of any one of them.
	huge := strings.Repeat("x", maxHistoryBytes*10)
	got := historyTurns([]repo.Message{{Role: "user", Content: huge}})
	if len(got) != 1 {
		t.Fatalf("want 1 turn, got %d", len(got))
	}
	if len(got[0].Content) > maxHistoryBytes+len("…") {
		t.Errorf("message not capped: %d bytes (cap %d)", len(got[0].Content), maxHistoryBytes)
	}
	if !strings.HasSuffix(got[0].Content, "…") {
		t.Error("a truncated message should be marked with an ellipsis")
	}
}

func TestHistoryTurnsLeavesSmallMessagesUntouched(t *testing.T) {
	got := historyTurns([]repo.Message{{Role: "assistant", Content: "short answer"}})
	if len(got) != 1 || got[0].Content != "short answer" {
		t.Fatalf("small message was altered: %#v", got)
	}
	if got[0].Role != "assistant" {
		t.Errorf("role not preserved: %q", got[0].Role)
	}
}

func TestHistoryTurnsCapDoesNotSplitARune(t *testing.T) {
	// Cutting mid-rune would put invalid UTF-8 into the JSON request body.
	// A run of 3-byte runes forces the cut onto a rune boundary.
	got := historyTurns([]repo.Message{{Role: "user", Content: strings.Repeat("あ", maxHistoryBytes)}})
	if !utf8.ValidString(got[0].Content) {
		t.Error("capped content is not valid UTF-8")
	}
}
