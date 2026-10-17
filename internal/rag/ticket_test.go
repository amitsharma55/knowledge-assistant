package rag

import "testing"

func TestIsTicketQuery(t *testing.T) {
	yes := []string{
		"latest open ticket for Team star",
		"any open tickets?",
		"what's the status of INC0012345",
		"show me the incident",
		"TICKET please",
	}
	no := []string{
		"how does AVR send fields to Coupa?",
		"what is the submission deadline?",
		"hello there",
	}
	for _, q := range yes {
		if !isTicketQuery(q) {
			t.Errorf("isTicketQuery(%q) = false, want true", q)
		}
	}
	for _, q := range no {
		if isTicketQuery(q) {
			t.Errorf("isTicketQuery(%q) = true, want false", q)
		}
	}
}
