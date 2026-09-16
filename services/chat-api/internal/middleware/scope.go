package middleware

import (
	"context"
	"net/http"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
)

type scopeCtxKey struct{}
type allowedCtxKey struct{}

// Resolver maps a user to the teams they belong to.
type Resolver interface {
	AllowedTeams(ctx context.Context, userID string) ([]team.Team, error)
}

// StaticResolver is the demo resolver: a hardcoded membership table. The
// production implementation reads Entra ID group claims from the verified JWT
// and maps group object IDs to teams; it satisfies this same interface, so
// swapping it touches only the wiring in cmd/server.
type StaticResolver struct {
	Registry *team.Registry
	Members  map[string][]string
}

func (s StaticResolver) AllowedTeams(_ context.Context, userID string) ([]team.Team, error) {
	var out []team.Team
	for _, slug := range s.Members[userID] {
		t, err := s.Registry.Parse(slug)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// DemoMembers is the membership table used in dev mode.
func DemoMembers() map[string][]string {
	return map[string][]string{
		"a@example.com":   {"coupa"},
		"b@example.com":   {"star", "hr"},
		"dev@example.com": {"coupa", "star", "hr"},
	}
}

// WithScope resolves the caller's teams, authorises the requested team from
// the X-Team header, and installs the resulting rag.Scope on the context.
// This is the only place team authorisation happens.
func WithScope(reg *team.Registry, res Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserFromContext(r.Context())
			allowed, err := res.AllowedTeams(r.Context(), userID)
			if err != nil {
				http.Error(w, "resolve teams", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), allowedCtxKey{}, allowed)

			// The teams listing needs the allowed set but has no active team.
			if r.URL.Path == "/v1/teams" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			requested, err := reg.Parse(r.Header.Get("X-Team"))
			if err == nil {
				err = team.Authorize(allowed, requested)
			}
			if err != nil {
				// Unknown and unauthorised are the same response: a caller
				// must not learn which teams exist by probing.
				http.Error(w, "team not available", http.StatusForbidden)
				return
			}

			scope := rag.NewScope(requested, allowed, GroupsFromContext(r.Context()))
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, scopeCtxKey{}, scope)))
		})
	}
}

// ContextWithAllowedForTest installs an allowed-team set without running the
// middleware. Test support only.
func ContextWithAllowedForTest(ctx context.Context, allowed []team.Team) context.Context {
	return context.WithValue(ctx, allowedCtxKey{}, allowed)
}

// ContextWithScopeForTest installs an active scope and user id without running
// the auth/scope middleware. Test support only: handlers that call
// ScopeFromContext/UserFromContext can be exercised directly.
func ContextWithScopeForTest(ctx context.Context, active team.Team, user string) context.Context {
	ctx = context.WithValue(ctx, userIDKey, user)
	scope := rag.NewScope(active, []team.Team{active}, nil)
	return context.WithValue(ctx, scopeCtxKey{}, scope)
}

func ScopeFromContext(ctx context.Context) (rag.Scope, bool) {
	s, ok := ctx.Value(scopeCtxKey{}).(rag.Scope)
	return s, ok && !s.IsZero()
}

func AllowedFromContext(ctx context.Context) ([]team.Team, bool) {
	ts, ok := ctx.Value(allowedCtxKey{}).([]team.Team)
	return ts, ok
}

func UserFromContext(ctx context.Context) string {
	if v := ctx.Value(userIDKey); v != nil {
		if u, ok := v.(string); ok {
			return u
		}
	}
	return ""
}
