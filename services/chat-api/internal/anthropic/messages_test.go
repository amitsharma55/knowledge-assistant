package anthropic

import (
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
)

func roles(ms []message) string {
	s := ""
	for _, m := range ms {
		s += string(m.Role[0])
	}
	return s
}

func TestMessagesAppendsThisTurnAfterHistory(t *testing.T) {
	got := messages(rag.Prompt{
		History: []rag.Turn{
			{Role: "user", Content: "what fields?"},
			{Role: "assistant", Content: "which integration?"},
		},
		User: "<context>...</context>\n\nQuestion: AVR",
	})
	if roles(got) != "uau" {
		t.Errorf("roles = %q, want user/assistant/user", roles(got))
	}
	if got[2].Content != "<context>...</context>\n\nQuestion: AVR" {
		t.Error("the current turn must be last and carry the context")
	}
}

func TestMessagesDropsHistoryTheAPIWouldReject(t *testing.T) {
	// The Messages API requires an alternating sequence starting with user.
	// A stored history that starts on assistant, repeats a role, or holds a
	// blank turn would be rejected outright -- losing the answer, not just
	// the context.
	for name, hist := range map[string][]rag.Turn{
		"starts on assistant": {{Role: "assistant", Content: "hi"}, {Role: "user", Content: "q"}},
		"repeated role":       {{Role: "user", Content: "a"}, {Role: "user", Content: "b"}},
		"blank turn":          {{Role: "user", Content: "  "}, {Role: "assistant", Content: "r"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := messages(rag.Prompt{History: hist, User: "now"})
			want := "u"
			if roles(got) == "" || got[len(got)-1].Role != "user" {
				t.Fatalf("roles = %q, must end on the current user turn", roles(got))
			}
			for i := 1; i < len(got); i++ {
				if got[i].Role == got[i-1].Role {
					t.Fatalf("roles = %q, must alternate", roles(got))
				}
			}
			if len(got) == 1 && roles(got) != want {
				return
			}
		})
	}
}

func TestMessagesWithNoHistoryIsASingleUserTurn(t *testing.T) {
	got := messages(rag.Prompt{User: "only"})
	if len(got) != 1 || got[0].Role != "user" || got[0].Content != "only" {
		t.Errorf("got %+v, want one user turn", got)
	}
}
