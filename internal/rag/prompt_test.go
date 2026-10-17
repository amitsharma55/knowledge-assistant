package rag

import (
	"strings"
	"testing"
)

func TestBuildTicketPromptIncludesTicketData(t *testing.T) {
	p := BuildTicketPrompt("latest open ticket for star", nil, []Ticket{{
		Number: "INC0012345", ShortDescription: "VPN down", State: "In Progress",
		Priority: "2 - High", UpdatedOn: "2026-10-01 14:05:00", URL: "https://sn/incident.do?sys_id=abc",
	}})
	if !strings.Contains(p.User, "INC0012345") || !strings.Contains(p.User, "VPN down") {
		t.Errorf("ticket prompt user text missing ticket data:\n%s", p.User)
	}
	if !strings.Contains(strings.ToLower(p.System), "ticket") {
		t.Errorf("ticket system prompt does not mention tickets:\n%s", p.System)
	}
}
