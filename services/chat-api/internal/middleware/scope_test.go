package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
)

func testResolver() (*team.Registry, Resolver) {
	reg := team.NewRegistry(team.DefaultInfos())
	return reg, StaticResolver{
		Registry: reg,
		Members: map[string][]string{
			"a@example.com": {"coupa"},
			"b@example.com": {"star", "hr"},
		},
	}
}

// probe records the scope the middleware installed.
func probe(t *testing.T, userID, teamHeader string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	reg, res := testResolver()
	var sawTeam string
	h := Auth(true)(WithScope(reg, res)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := ScopeFromContext(r.Context())
		if !ok {
			t.Error("handler reached with no scope in context")
			return
		}
		sawTeam = s.Team().Slug()
	})))
	req := httptest.NewRequest(http.MethodGet, "/v1/chats", nil)
	req.Header.Set("X-Dev-User", userID)
	if teamHeader != "" {
		req.Header.Set("X-Team", teamHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, sawTeam
}

func TestScopeInstalledForAMemberTeam(t *testing.T) {
	rec, sawTeam := probe(t, "b@example.com", "hr")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if sawTeam != "hr" {
		t.Errorf("scope team = %q, want hr", sawTeam)
	}
}

func TestForgedTeamIsRejectedWithoutReachingTheHandler(t *testing.T) {
	rec, sawTeam := probe(t, "b@example.com", "coupa")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a team the caller is not in", rec.Code)
	}
	if sawTeam != "" {
		t.Errorf("handler ran with team %q; a forged team must not reach retrieval", sawTeam)
	}
}

func TestUnknownTeamIsRejected(t *testing.T) {
	rec, _ := probe(t, "b@example.com", "finance")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for an unregistered team", rec.Code)
	}
}

func TestMissingTeamHeaderIsRejectedNotDefaulted(t *testing.T) {
	rec, sawTeam := probe(t, "b@example.com", "")
	if rec.Code == http.StatusOK {
		t.Error("request with no X-Team succeeded; a missing team must not silently default")
	}
	if sawTeam != "" {
		t.Errorf("handler ran with team %q despite no X-Team header", sawTeam)
	}
}

// TestProdModeRejectsBeforeScopeResolution verifies that Auth(false) 401s the
// request before WithScope ever runs, regardless of what headers are sent.
//
// This does NOT prove that X-Dev-User is ignored in prod mode: Auth(false)
// currently rejects every request unconditionally (there is no real JWT
// verification yet), so this test would pass identically with no
// X-Dev-User header at all. A genuine "X-Dev-User is ignored in prod" test
// is not writable until Auth(false) does real JWT verification and could
// plausibly reach WithScope with a forged X-Dev-User header still present.
func TestProdModeRejectsBeforeScopeResolution(t *testing.T) {
	reg := team.NewRegistry(team.DefaultInfos())
	res := StaticResolver{
		Registry: reg,
		Members: map[string][]string{
			"b@example.com": {"star", "hr"},
		},
	}
	h := Auth(false)(WithScope(reg, res)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not run in prod mode with no valid auth")
	})))
	req := httptest.NewRequest(http.MethodGet, "/v1/chats", nil)
	req.Header.Set("X-Dev-User", "b@example.com")
	req.Header.Set("X-Team", "hr")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("prod mode should 401 on missing real auth, got %d", rec.Code)
	}
}
