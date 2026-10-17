// Package servicenow is a thin client for the ServiceNow Table API. It fetches
// open incidents for an assignment group and, via Create, files a new one; the
// write happens only behind the chat confirm-before-file flow.
package servicenow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Incident is the subset of a ServiceNow incident this app summarizes.
type Incident struct {
	Number           string
	ShortDescription string
	State            string
	Priority         string
	UpdatedOn        string
	OpenedOn         string
	URL              string
}

// Client talks to one ServiceNow instance with basic auth.
type Client struct {
	BaseURL  string
	User     string
	Password string
	HTTP     *http.Client
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// LatestOpen returns up to limit active incidents for the assignment group,
// newest-updated first. limit <= 0 defaults to 5.
func (c Client) LatestOpen(ctx context.Context, group string, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 5
	}
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/api/now/table/incident")
	if err != nil {
		return nil, fmt.Errorf("servicenow: parse base url %q: %w", c.BaseURL, err)
	}
	q := url.Values{}
	// Encoded-query grammar: name=value pairs joined by ^, ORDERBYDESC<field>
	// for sort. url.Values.Encode escapes the spaces in the group name for us.
	q.Set("sysparm_query", "assignment_group.name="+group+"^active=true^ORDERBYDESCsys_updated_on")
	q.Set("sysparm_limit", strconv.Itoa(limit))
	q.Set("sysparm_display_value", "true")
	q.Set("sysparm_fields", "number,short_description,state,priority,sys_updated_on,opened_at,sys_id")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("servicenow: %w", err)
	}
	req.SetBasicAuth(c.User, c.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("servicenow: GET incidents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("servicenow: %s returned %d: %s (check KA_SERVICENOW_URL and credentials)",
			u.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Result []struct {
			Number           string `json:"number"`
			ShortDescription string `json:"short_description"`
			State            string `json:"state"`
			Priority         string `json:"priority"`
			SysUpdatedOn     string `json:"sys_updated_on"`
			OpenedAt         string `json:"opened_at"`
			SysID            string `json:"sys_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("servicenow: decode response: %w", err)
	}
	base := strings.TrimSuffix(c.BaseURL, "/")
	incs := make([]Incident, 0, len(out.Result))
	for _, r := range out.Result {
		incs = append(incs, Incident{
			Number:           r.Number,
			ShortDescription: r.ShortDescription,
			State:            r.State,
			Priority:         r.Priority,
			UpdatedOn:        r.SysUpdatedOn,
			OpenedOn:         r.OpenedAt,
			URL:              base + "/incident.do?sys_id=" + r.SysID,
		})
	}
	return incs, nil
}

// Create files a new incident for the assignment group and returns it (number
// and link). group and priority are sent as display values
// (sysparm_input_display_value=true), matching the read query's convention, so
// the group goes by name and the priority by its number. caller is best-effort:
// omitted when empty, since the dev identity isn't yet a resolvable sys_user.
func (c Client) Create(ctx context.Context, group, shortDescription, priority, caller string) (Incident, error) {
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/api/now/table/incident")
	if err != nil {
		return Incident{}, fmt.Errorf("servicenow: parse base url %q: %w", c.BaseURL, err)
	}
	q := url.Values{}
	// Input display values so assignment_group matches by name and priority by
	// its number, the same display convention LatestOpen reads back.
	q.Set("sysparm_input_display_value", "true")
	q.Set("sysparm_fields", "number,short_description,state,priority,sys_updated_on,opened_at,sys_id")
	u.RawQuery = q.Encode()

	payload := map[string]string{
		"short_description": shortDescription,
		"assignment_group":  group,
		"priority":          priority,
	}
	if caller != "" {
		payload["caller_id"] = caller
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Incident{}, fmt.Errorf("servicenow: encode incident: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return Incident{}, fmt.Errorf("servicenow: %w", err)
	}
	req.SetBasicAuth(c.User, c.Password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return Incident{}, fmt.Errorf("servicenow: POST incident: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return Incident{}, fmt.Errorf("servicenow: %s returned %d: %s (the integration user needs incident create rights)",
			u.Path, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out struct {
		Result struct {
			Number           string `json:"number"`
			ShortDescription string `json:"short_description"`
			State            string `json:"state"`
			Priority         string `json:"priority"`
			SysUpdatedOn     string `json:"sys_updated_on"`
			OpenedAt         string `json:"opened_at"`
			SysID            string `json:"sys_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Incident{}, fmt.Errorf("servicenow: decode create response: %w", err)
	}
	r := out.Result
	return Incident{
		Number:           r.Number,
		ShortDescription: r.ShortDescription,
		State:            r.State,
		Priority:         r.Priority,
		UpdatedOn:        r.SysUpdatedOn,
		OpenedOn:         r.OpenedAt,
		URL:              strings.TrimSuffix(c.BaseURL, "/") + "/incident.do?sys_id=" + r.SysID,
	}, nil
}

// openedSinceExpr maps a window key to a ServiceNow encoded-query bound on
// opened_at, using server-side relative-date functions (computed in the
// instance timezone, so "today"/"this week" boundaries are correct). "days"
// uses n; the named keys ignore it. An unknown key is an error.
func openedSinceExpr(windowKey string, days int) (string, error) {
	switch windowKey {
	case "today":
		return "opened_at>=javascript:gs.beginningOfToday()", nil
	case "this_week":
		return "opened_at>=javascript:gs.beginningOfThisWeek()", nil
	case "this_month":
		return "opened_at>=javascript:gs.beginningOfThisMonth()", nil
	case "days":
		if days <= 0 {
			days = 7
		}
		return fmt.Sprintf("opened_at>=javascript:gs.daysAgoStart(%d)", days), nil
	default:
		return "", fmt.Errorf("servicenow: unknown count window %q", windowKey)
	}
}

// CountOpenedSince returns how many incidents were opened for the assignment
// group within the window, via the Aggregate (stats) API — a count only, no
// rows fetched. windowKey is one of today/this_week/this_month/days (days uses
// the days argument).
func (c Client) CountOpenedSince(ctx context.Context, group, windowKey string, days int) (int, error) {
	since, err := openedSinceExpr(windowKey, days)
	if err != nil {
		return 0, err
	}
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/api/now/stats/incident")
	if err != nil {
		return 0, fmt.Errorf("servicenow: parse base url %q: %w", c.BaseURL, err)
	}
	q := url.Values{}
	q.Set("sysparm_query", "assignment_group.name="+group+"^"+since)
	q.Set("sysparm_count", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("servicenow: %w", err)
	}
	req.SetBasicAuth(c.User, c.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return 0, fmt.Errorf("servicenow: GET incident count: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return 0, fmt.Errorf("servicenow: %s returned %d: %s (check KA_SERVICENOW_URL and credentials)",
			u.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// The stats API returns the count as a string.
	var out struct {
		Result struct {
			Stats struct {
				Count string `json:"count"`
			} `json:"stats"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("servicenow: decode count response: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out.Result.Stats.Count))
	if err != nil {
		return 0, fmt.Errorf("servicenow: parse count %q: %w", out.Result.Stats.Count, err)
	}
	return n, nil
}
