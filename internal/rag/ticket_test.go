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

func TestIsCountQuery(t *testing.T) {
	yes := []string{
		"how many tickets opened since last week",
		"how many incidents this month",
		"count of tickets opened today",
		"how many tickets were opened in the last 30 days",
	}
	no := []string{
		"how many open tickets do I have", // no time window -> read branch counts them
		"show me open tickets",
		"create a ticket",
		"how does AVR send fields to Coupa?",
	}
	for _, q := range yes {
		if !isCountQuery(q) {
			t.Errorf("isCountQuery(%q) = false, want true", q)
		}
	}
	for _, q := range no {
		if isCountQuery(q) {
			t.Errorf("isCountQuery(%q) = true, want false", q)
		}
	}
}

func TestParseCountWindow(t *testing.T) {
	cases := []struct {
		msg      string
		wantKey  string
		wantDays int
	}{
		{"how many tickets opened since last week", "days", 7},
		{"how many incidents this month", "this_month", 0},
		{"count of tickets opened today", "today", 0},
		{"how many tickets were opened in the last 30 days", "days", 30},
		{"how many incidents this week", "this_week", 0},
	}
	for _, tc := range cases {
		w := parseCountWindow(tc.msg)
		if w.Key != tc.wantKey || w.Days != tc.wantDays {
			t.Errorf("parseCountWindow(%q) = {Key:%q Days:%d}, want {Key:%q Days:%d}", tc.msg, w.Key, w.Days, tc.wantKey, tc.wantDays)
		}
		if w.Label == "" {
			t.Errorf("parseCountWindow(%q) returned an empty Label", tc.msg)
		}
	}
}
