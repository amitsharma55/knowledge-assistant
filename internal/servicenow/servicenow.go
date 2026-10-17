// Package servicenow is a thin, read-only client for the ServiceNow Table API.
// It fetches open incidents for an assignment group; it never writes.
package servicenow

import (
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
	q.Set("sysparm_fields", "number,short_description,state,priority,sys_updated_on,sys_id")
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
			URL:              base + "/incident.do?sys_id=" + r.SysID,
		})
	}
	return incs, nil
}
