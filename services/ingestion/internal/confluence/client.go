package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Page struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	BodyHTML  string    `json:"bodyHtml"`
	WebURL    string    `json:"webUrl"`
	UpdatedAt time.Time `json:"updatedAt"`
	Version   int       `json:"version"`
	// Restrictions expressed as group keys allowed to view.
	ACLGroups []string `json:"aclGroups"`
}

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// ListChangedPages returns pages in the space modified since `since`.
// Uses Confluence Cloud REST v2. In prod, page through cursors.
func (c *Client) ListChangedPages(ctx context.Context, spaceKey string, since time.Time) ([]Page, error) {
	q := url.Values{
		"cql":    []string{fmt.Sprintf(`space = "%s" AND type = "page" AND lastModified > "%s"`, spaceKey, since.UTC().Format("2006-01-02 15:04"))},
		"expand": []string{"body.storage,version,restrictions.read.restrictions.group"},
		"limit":  []string{"100"},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/wiki/rest/api/content/search?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("confluence %d", resp.StatusCode)
	}
	var raw struct {
		Results []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Space struct {
				Key string `json:"key"`
			} `json:"space"`
			Body struct {
				Storage struct {
					Value string `json:"value"`
				} `json:"storage"`
			} `json:"body"`
			Version struct {
				Number int       `json:"number"`
				When   time.Time `json:"when"`
			} `json:"version"`
			Links struct {
				Webui string `json:"webui"`
			} `json:"_links"`
			Restrictions struct {
				Read struct {
					Restrictions struct {
						Group struct {
							Results []struct {
								Name string `json:"name"`
							} `json:"results"`
						} `json:"group"`
					} `json:"restrictions"`
				} `json:"read"`
			} `json:"restrictions"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	pages := make([]Page, 0, len(raw.Results))
	for _, r := range raw.Results {
		var groups []string
		for _, g := range r.Restrictions.Read.Restrictions.Group.Results {
			groups = append(groups, g.Name)
		}
		pages = append(pages, Page{
			ID:        r.ID,
			Title:     r.Title,
			SpaceKey:  r.Space.Key,
			BodyHTML:  r.Body.Storage.Value,
			WebURL:    c.BaseURL + "/wiki" + r.Links.Webui,
			UpdatedAt: r.Version.When,
			Version:   r.Version.Number,
			ACLGroups: groups,
		})
	}
	return pages, nil
}
