package servicenow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestOpenQueryAndMapping(t *testing.T) {
	var gotQuery, gotAuthUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("sysparm_query")
		u, _, _ := r.BasicAuth()
		gotAuthUser = u
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]string{{
			"number": "INC0012345", "short_description": "VPN down",
			"state": "In Progress", "priority": "2 - High",
			"sys_updated_on": "2026-10-01 14:05:00", "opened_at": "2026-10-01 14:05:00",
			"sys_id": "abc123",
		}}})
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, User: "svc", Password: "pw", HTTP: srv.Client()}
	got, err := c.LatestOpen(context.Background(), "Star Support", 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	// Group with a space must be carried intact in the decoded query.
	if !strings.Contains(gotQuery, "assignment_group.name=Star Support") {
		t.Errorf("sysparm_query = %q, want it to contain the group name verbatim", gotQuery)
	}
	if !strings.Contains(gotQuery, "active=true") || !strings.Contains(gotQuery, "ORDERBYDESCsys_updated_on") {
		t.Errorf("sysparm_query = %q, want active=true and ORDERBYDESCsys_updated_on", gotQuery)
	}
	if gotAuthUser != "svc" {
		t.Errorf("basic-auth user = %q, want svc", gotAuthUser)
	}
	if len(got) != 1 {
		t.Fatalf("got %d incidents, want 1", len(got))
	}
	if got[0].Number != "INC0012345" || got[0].State != "In Progress" {
		t.Errorf("mapped incident = %+v", got[0])
	}
	if got[0].OpenedOn != "2026-10-01 14:05:00" {
		t.Errorf("OpenedOn = %q, want the opened_at value", got[0].OpenedOn)
	}
	if !strings.HasPrefix(got[0].URL, srv.URL) || !strings.Contains(got[0].URL, "abc123") {
		t.Errorf("incident URL = %q, want it to point at sys_id abc123", got[0].URL)
	}
}

func TestLatestOpenRequestsOpenedAtField(t *testing.T) {
	var gotFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFields = r.URL.Query().Get("sysparm_fields")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]string{}})
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
	_, _ = c.LatestOpen(context.Background(), "G", 5)
	if !strings.Contains(gotFields, "opened_at") {
		t.Errorf("sysparm_fields = %q, want it to include opened_at", gotFields)
	}
}

func TestCountOpenedSinceQueryAndCount(t *testing.T) {
	var gotPath, gotQuery, gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("sysparm_query")
		gotCount = r.URL.Query().Get("sysparm_count")
		// ServiceNow's stats API returns count as a string.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"stats": map[string]string{"count": "3"}},
		})
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "svc", Password: "pw", HTTP: srv.Client()}
	n, err := c.CountOpenedSince(context.Background(), "Star Reporting", "days", 7)
	if err != nil {
		t.Fatalf("CountOpenedSince: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	if gotPath != "/api/now/stats/incident" {
		t.Errorf("path = %q, want the aggregate stats endpoint", gotPath)
	}
	if gotCount != "true" {
		t.Errorf("sysparm_count = %q, want true", gotCount)
	}
	if !strings.Contains(gotQuery, "assignment_group.name=Star Reporting") ||
		!strings.Contains(gotQuery, "opened_at>=javascript:gs.daysAgoStart(7)") {
		t.Errorf("sysparm_query = %q, want the group and a 7-days-ago opened_at bound", gotQuery)
	}
}

func TestCountOpenedSinceWindowExpressions(t *testing.T) {
	cases := map[string]string{
		"today":      "opened_at>=javascript:gs.beginningOfToday()",
		"this_week":  "opened_at>=javascript:gs.beginningOfThisWeek()",
		"this_month": "opened_at>=javascript:gs.beginningOfThisMonth()",
	}
	for key, want := range cases {
		var gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query().Get("sysparm_query")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"stats": map[string]string{"count": "0"}}})
		}))
		c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
		if _, err := c.CountOpenedSince(context.Background(), "G", key, 0); err != nil {
			t.Fatalf("CountOpenedSince(%s): %v", key, err)
		}
		if !strings.Contains(gotQuery, want) {
			t.Errorf("window %q query = %q, want it to contain %q", key, gotQuery, want)
		}
		srv.Close()
	}
}

func TestCountOpenedSinceErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
	if _, err := c.CountOpenedSince(context.Background(), "G", "days", 7); err == nil {
		t.Fatal("CountOpenedSince on 403 = nil error, want an error")
	}
}

func TestLatestOpenErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
	if _, err := c.LatestOpen(context.Background(), "Star Support", 5); err == nil {
		t.Fatal("LatestOpen on 401 = nil error, want an error")
	}
}

func TestLatestOpenEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]string{}})
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, User: "x", Password: "y", HTTP: srv.Client()}
	got, err := c.LatestOpen(context.Background(), "Star Support", 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d incidents, want 0", len(got))
	}
}
