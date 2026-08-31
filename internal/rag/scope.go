package rag

import "github.com/example/knowledge-assistant/internal/team"

// Scope is the resolved retrieval scope for one request: which team's corpus
// is in play, which other teams the caller may switch to, and the caller's
// ACL groups.
//
// Its fields are unexported and it requires a team.Team, which only
// team.Registry.Parse can produce. A handler therefore cannot turn a string
// from a request into a Scope. Authorisation itself lives in team.Authorize,
// called once, in middleware.
type Scope struct {
	active  team.Team
	allowed []team.Team
	groups  []string
}

func NewScope(active team.Team, allowed []team.Team, groups []string) Scope {
	return Scope{active: active, allowed: allowed, groups: groups}
}

func (s Scope) Team() team.Team  { return s.active }
func (s Scope) Groups() []string { return s.groups }
func (s Scope) IsZero() bool     { return s.active.IsZero() }

// Others returns the caller's allowed teams excluding the active one. It backs
// the no-results escape hatch.
func (s Scope) Others() []team.Team {
	out := make([]team.Team, 0, len(s.allowed))
	for _, a := range s.allowed {
		if a != s.active {
			out = append(out, a)
		}
	}
	return out
}
