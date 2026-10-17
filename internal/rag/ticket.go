package rag

import (
	"context"
	"errors"
	"regexp"
	"strconv"
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
	OpenedOn         string
	URL              string
}

// CountWindow is a time window for a "how many opened since…" count. Key is one
// of today/this_week/this_month/days (days uses Days); Label is the user-facing
// phrase echoed in the answer ("the last 7 days").
type CountWindow struct {
	Key   string
	Days  int
	Label string
}

// ErrTeamNotConfigured means the caller's team has no ServiceNow group, so a
// ticket lookup is impossible — distinct from a transport failure.
var ErrTeamNotConfigured = errors.New("rag: team has no ticket source configured")

// TicketSource fetches a team's open tickets. The team comes from the request
// scope, never the message text.
type TicketSource interface {
	LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error)
	// CountOpened reports how many incidents were opened for the team within
	// the window.
	CountOpened(ctx context.Context, t team.Team, w CountWindow) (int, error)
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

var lastNDays = regexp.MustCompile(`(?i)(?:last|past)\s+(\d+)\s+days?`)

// isCountQuery is true for a date-scoped count question: "how many" or "count"
// about tickets/incidents, with a time window present. Without a time window
// ("how many open tickets do I have") it is false, so such a question falls
// through to the read branch, which lists the tickets. Requiring a window is
// what keeps a "how many open right now" question out of this opened-since path.
func isCountQuery(msg string) bool {
	low := strings.ToLower(msg)
	if !strings.Contains(low, "how many") && !strings.Contains(low, "count") {
		return false
	}
	if !strings.Contains(low, "ticket") && !strings.Contains(low, "incident") {
		return false
	}
	return hasTimeWindow(low)
}

// hasTimeWindow reports whether the message names a window parseCountWindow can
// resolve. Kept in lockstep with parseCountWindow so the gate never admits a
// phrasing the parser would silently coerce to a wrong window.
func hasTimeWindow(low string) bool {
	return strings.Contains(low, "today") ||
		strings.Contains(low, "week") ||
		strings.Contains(low, "month") ||
		strings.Contains(low, "day") ||
		strings.Contains(low, "since")
}

// parseCountWindow maps the message to a CountWindow. The named windows
// (today/this week/this month) use ServiceNow's timezone-correct boundaries;
// everything else is a trailing-days lookback, defaulting to 7.
func parseCountWindow(msg string) CountWindow {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "today"):
		return CountWindow{Key: "today", Label: "today"}
	case strings.Contains(low, "this week"):
		return CountWindow{Key: "this_week", Label: "this week"}
	case strings.Contains(low, "this month"):
		return CountWindow{Key: "this_month", Label: "this month"}
	}
	if m := lastNDays.FindStringSubmatch(low); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return CountWindow{Key: "days", Days: n, Label: "in the last " + m[1] + " days"}
		}
	}
	if strings.Contains(low, "month") {
		return CountWindow{Key: "days", Days: 30, Label: "in the last 30 days"}
	}
	// "last week", "past week", "since last week", or an unmapped window.
	return CountWindow{Key: "days", Days: 7, Label: "in the last 7 days"}
}
