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

// TicketDraft is the two fields the create flow collects before filing. The
// team (and thus assignment_group) is never in here — it comes from the request
// scope, never the message text.
type TicketDraft struct {
	ShortDescription string
	Priority         string // ServiceNow priority number: "1".."4"
}

// TicketSource fetches a team's open tickets. The team comes from the request
// scope, never the message text.
type TicketSource interface {
	LatestOpen(ctx context.Context, t team.Team, limit int) ([]Ticket, error)
	// CountOpened reports how many incidents were opened for the team within
	// the window.
	CountOpened(ctx context.Context, t team.Team, w CountWindow) (int, error)
	// Create files a new incident for the team and returns it (number + URL).
	Create(ctx context.Context, t team.Team, d TicketDraft) (Ticket, error)
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

// The create flow's prompt strings are constants, because phase detection
// recognizes an in-progress create by matching the last assistant turn against
// them. Known heuristic ceiling: a user pasting one of these back confuses it.
const (
	issuePrompt    = "What's the issue you'd like to report? Describe it in one line."
	severityPrompt = "How severe is it? Reply Critical, High, Moderate, or Low."
	// draftPrefix starts every draft so createInProgress/phase detection can
	// recognize it without storing state.
	draftPrefix = "Draft incident for "
)

// "open"/"file"/"log" are read words too ("open tickets", "log of incidents"),
// so they only read as a create verb with an article after them.
var openCreateRe = regexp.MustCompile(`\b(open|file|log)\s+(a|an|another|new)\b`)

// isCreateTicketQuery is true when the message asks to file a new ticket. It is
// deterministic by design: a write must not fire on model judgement. Checked
// before the read heuristic, so the ambiguous verbs are deliberately narrow —
// "any open tickets?" is a read, not a create.
func isCreateTicketQuery(msg string) bool {
	low := strings.ToLower(msg)
	if !strings.Contains(low, "ticket") && !strings.Contains(low, "incident") {
		return false
	}
	// Unambiguous create verbs.
	if strings.Contains(low, "create") || strings.Contains(low, "raise") || strings.Contains(low, "submit") {
		return true
	}
	return openCreateRe.MatchString(low)
}

// createInProgress is true when the last assistant turn is one of the create
// flow's own prompts, which is what carries the slot answers and the confirm
// (none of which contain a create keyword) back into the create branch.
func createInProgress(history []Turn) bool {
	last := lastAssistant(history)
	return last == issuePrompt || last == severityPrompt || strings.HasPrefix(last, draftPrefix)
}

// priorityFor maps a severity word to a ServiceNow priority number. ok is false
// for an unrecognized word, which makes the flow re-ask rather than advance.
// Checked critical→low so "critical"/"high" win over a stray "low" in the text.
func priorityFor(msg string) (string, bool) {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "critical"):
		return "1", true
	case strings.Contains(low, "high"):
		return "2", true
	case strings.Contains(low, "moderate"), strings.Contains(low, "medium"):
		return "3", true
	case strings.Contains(low, "low"):
		return "4", true
	}
	return "", false
}

// priorityLabel is the display word for a priority number, for the draft.
func priorityLabel(p string) string {
	switch p {
	case "1":
		return "Critical"
	case "2":
		return "High"
	case "3":
		return "Moderate"
	case "4":
		return "Low"
	}
	return p
}

// inlineIssue extracts an issue stated inline with the create request
// ("create a ticket: email server down"). Empty when there is no colon form.
func inlineIssue(msg string) string {
	i := strings.Index(msg, ":")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(msg[i+1:])
}

// issueFromMessage is the short_description a user-turn carries: the inline part
// after the colon when the turn is itself a create request, else the whole turn.
func issueFromMessage(msg string) string {
	if isCreateTicketQuery(msg) {
		if in := inlineIssue(msg); in != "" {
			return in
		}
	}
	return strings.TrimSpace(msg)
}

// draftText is the draft shown before filing. It names the team (the real
// assignment_group is resolved from scope at file time, not parsed from text).
func draftText(teamSlug, issue, priority string) string {
	return draftPrefix + teamSlug + ":\n" +
		"- Issue: " + issue + "\n" +
		"- Priority: " + priorityLabel(priority) + "\n\n" +
		"Reply **confirm** to file it, or **cancel**."
}

func isAffirmative(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "confirm", "yes", "y":
		return true
	}
	return false
}

func isCancel(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "cancel", "no", "n":
		return true
	}
	return false
}

func lastAssistant(history []Turn) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			return history[i].Content
		}
	}
	return ""
}

// userBefore returns the user turn immediately preceding the most recent
// assistant turn whose content satisfies match — i.e. the message that was
// answered with that prompt.
func userBefore(history []Turn, match func(string) bool) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" && match(history[i].Content) {
			if i > 0 && history[i-1].Role == "user" {
				return history[i-1].Content
			}
			return ""
		}
	}
	return ""
}

// draftFromHistory derives the create flow's current phase, and the issue and
// priority it has collected so far, from (history, message) alone — there is no
// stored state. Pure; table-tested. Phases:
//
//	"issue"    emit the issue prompt (fresh request, no inline issue)
//	"severity" emit the severity prompt (issue just given, or re-ask on an
//	           unmapped severity word)
//	"draft"    emit the draft (issue + mapped priority in hand)
//	"confirm"  last turn was a draft; caller inspects the message for
//	           confirm/cancel (issue + priority reconstructed for the file)
func draftFromHistory(history []Turn, message string) (phase, issue, priority string) {
	switch last := lastAssistant(history); {
	case last == issuePrompt:
		return "severity", "", ""
	case last == severityPrompt:
		if p, ok := priorityFor(message); ok {
			return "draft", issueFromHistory(history), p
		}
		return "severity", "", "" // unmapped word: re-ask, don't advance
	case strings.HasPrefix(last, draftPrefix):
		return "confirm", issueFromHistory(history), priorityFromHistory(history)
	default:
		if inlineIssue(message) != "" {
			return "severity", "", ""
		}
		return "issue", "", ""
	}
}

// issueFromHistory reconstructs the short_description: the user turn that was
// answered with the severity prompt.
func issueFromHistory(history []Turn) string {
	return issueFromMessage(userBefore(history, func(c string) bool { return c == severityPrompt }))
}

// priorityFromHistory reconstructs the priority: the user turn that was answered
// with the draft.
func priorityFromHistory(history []Turn) string {
	p, _ := priorityFor(userBefore(history, func(c string) bool { return strings.HasPrefix(c, draftPrefix) }))
	return p
}
