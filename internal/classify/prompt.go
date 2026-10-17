// Package classify decides whether a user message is a support-ticket query,
// the optional LLM fallback of the orchestrator's intent gate.
package classify

import (
	"encoding/json"
	"fmt"
	"strings"
)

const systemPrompt = `Decide whether the user's message is asking about IT support tickets or incidents — for example open tickets, incident status, outages, or a specific INC number. Reply with JSON only: {"ticket": true} or {"ticket": false}. No other text.`

// parseTicketBool reads {"ticket": bool} out of a reply, tolerating prose around
// the JSON (as Bedrock's Converse may add).
func parseTicketBool(content string) (bool, error) {
	i, j := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if i < 0 || j < i {
		return false, fmt.Errorf("classify: no JSON object in %q", content)
	}
	var parsed struct {
		Ticket bool `json:"ticket"`
	}
	if err := json.Unmarshal([]byte(content[i:j+1]), &parsed); err != nil {
		return false, fmt.Errorf("classify: decode %q: %w", content[i:j+1], err)
	}
	return parsed.Ticket, nil
}
