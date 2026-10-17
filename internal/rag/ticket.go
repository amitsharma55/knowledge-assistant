package rag

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/example/knowledge-assistant/internal/team"
)

// Ticket is one open ServiceNow incident, as summarized for the user.
type Ticket struct {
	Number           string
	ShortDescription string
	State            string
	Priority         string
	UpdatedOn        string
	URL              string
}

// ErrTeamNotConfigured means the caller's team has no ServiceNow group, so a
// ticket lookup is impossible — distinct from a transport failure.
var ErrTeamNotConfigured = errors.New("rag: team has no ticket source configured")

// TicketSource fetches a team's open tickets. The team comes from the request
// scope, never the message text.
type TicketSource interface {
	LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error)
}

// Classifier reports whether a message is a ticket query. Optional fallback for
// phrasing the heuristic misses.
type Classifier interface {
	Classify(ctx context.Context, question string) (bool, error)
}

var incidentNumber = regexp.MustCompile(`(?i)\bINC\d+\b`)

// isTicketQuery is the heuristic fast-path of the intent gate: an INC number or
// the words "ticket"/"incident". Deterministic and cheap; the optional
// Classifier catches the rest.
func isTicketQuery(msg string) bool {
	if incidentNumber.MatchString(msg) {
		return true
	}
	low := strings.ToLower(msg)
	return strings.Contains(low, "ticket") || strings.Contains(low, "incident")
}
