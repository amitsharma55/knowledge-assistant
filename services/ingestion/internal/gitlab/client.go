// Package gitlab is a minimal client for pulling markdown docs out of a
// self-managed GitLab instance. Supports two sources:
//
//   - Repo markdown: *.md files under a path (default: docs/) on a ref
//   - Project wiki:  markdown wiki pages on a project
//
// Both flow through the same normalized Page struct so the ingester doesn't
// care where a page came from.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL     string // e.g. https://gitlab.acme.com
	Token       string // Personal or Project access token with read_repository + read_wiki
	ProjectID   string // numeric ID or path like "group/sub/project"
	RepoRef     string // branch/tag; "" = repository default
	RepoPath    string // path prefix, e.g. "docs"; "" = whole tree
	IncludeRepo bool
	IncludeWiki bool
}

type Client struct {
	cfg  Config
	HTTP *http.Client
}

// Page mirrors the shape the ingester expects, regardless of source.
type Page struct {
	ProjectID string
	Source    string // "repo" or "wiki"
	ID        string
	Title     string
	Path      string
	Markdown  string
	WebURL    string
	UpdatedAt time.Time
	ACLGroups []string
}

func New(cfg Config) *Client {
	if cfg.RepoPath == "" {
		cfg.RepoPath = "docs"
	}
	return &Client{cfg: cfg, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// FetchAll returns every doc page requested by the config.
func (c *Client) FetchAll(ctx context.Context) ([]Page, error) {
	var out []Page
	if c.cfg.IncludeRepo {
		p, err := c.fetchRepo(ctx)
		if err != nil {
			return nil, fmt.Errorf("repo: %w", err)
		}
		out = append(out, p...)
	}
	if c.cfg.IncludeWiki {
		p, err := c.fetchWiki(ctx)
		if err != nil {
			return nil, fmt.Errorf("wiki: %w", err)
		}
		out = append(out, p...)
	}
	return out, nil
}

// --- repo files ---

type treeEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "blob" | "tree"
	Path string `json:"path"`
}

type commitInfo struct {
	CommittedDate time.Time `json:"committed_date"`
}

func (c *Client) fetchRepo(ctx context.Context) ([]Page, error) {
	proj := url.PathEscape(c.cfg.ProjectID)
	ref := c.cfg.RepoRef
	if ref == "" {
		ref = "HEAD"
	}
	var entries []treeEntry
	page := 1
	for {
		u := fmt.Sprintf("%s/api/v4/projects/%s/repository/tree?recursive=true&per_page=100&page=%d&path=%s&ref=%s",
			strings.TrimRight(c.cfg.BaseURL, "/"), proj, page, url.QueryEscape(c.cfg.RepoPath), url.QueryEscape(ref))
		var batch []treeEntry
		next, err := c.getJSON(ctx, u, &batch)
		if err != nil {
			return nil, err
		}
		entries = append(entries, batch...)
		if next == "" {
			break
		}
		page++
	}

	var out []Page
	for _, e := range entries {
		if e.Type != "blob" {
			continue
		}
		if !isMarkdown(e.Name) {
			continue
		}
		body, err := c.getRaw(ctx,
			fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s/raw?ref=%s",
				strings.TrimRight(c.cfg.BaseURL, "/"), proj, url.PathEscape(e.Path), url.QueryEscape(ref)))
		if err != nil {
			return nil, fmt.Errorf("get file %s: %w", e.Path, err)
		}
		updated := c.lastCommitDate(ctx, proj, e.Path, ref)
		out = append(out, Page{
			ProjectID: c.cfg.ProjectID,
			Source:    "repo",
			ID:        "repo:" + e.Path,
			Title:     titleFromPath(e.Path),
			Path:      e.Path,
			Markdown:  string(body),
			WebURL: fmt.Sprintf("%s/%s/-/blob/%s/%s",
				strings.TrimRight(c.cfg.BaseURL, "/"), c.cfg.ProjectID, url.PathEscape(ref), e.Path),
			UpdatedAt: updated,
		})
	}
	return out, nil
}

// lastCommitDate is best-effort — returns zero time on any error so we don't
// fail the whole ingest just because a single file's commit lookup fails.
func (c *Client) lastCommitDate(ctx context.Context, proj, path, ref string) time.Time {
	u := fmt.Sprintf("%s/api/v4/projects/%s/repository/commits?path=%s&ref_name=%s&per_page=1",
		strings.TrimRight(c.cfg.BaseURL, "/"), proj, url.QueryEscape(path), url.QueryEscape(ref))
	var commits []commitInfo
	if _, err := c.getJSON(ctx, u, &commits); err != nil || len(commits) == 0 {
		return time.Time{}
	}
	return commits[0].CommittedDate
}

// --- wiki pages ---

type wikiStub struct {
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	Format  string `json:"format"`
	Content string `json:"content"`
}

func (c *Client) fetchWiki(ctx context.Context) ([]Page, error) {
	proj := url.PathEscape(c.cfg.ProjectID)
	var stubs []wikiStub
	if _, err := c.getJSON(ctx,
		fmt.Sprintf("%s/api/v4/projects/%s/wikis", strings.TrimRight(c.cfg.BaseURL, "/"), proj),
		&stubs); err != nil {
		return nil, err
	}
	var out []Page
	for _, s := range stubs {
		if s.Format != "" && s.Format != "markdown" {
			continue // skip asciidoc / rdoc etc. — v1 is markdown only
		}
		var full wikiStub
		if _, err := c.getJSON(ctx,
			fmt.Sprintf("%s/api/v4/projects/%s/wikis/%s?render_html=false",
				strings.TrimRight(c.cfg.BaseURL, "/"), proj, url.PathEscape(s.Slug)),
			&full); err != nil {
			return nil, fmt.Errorf("get wiki %s: %w", s.Slug, err)
		}
		out = append(out, Page{
			ProjectID: c.cfg.ProjectID,
			Source:    "wiki",
			ID:        "wiki:" + full.Slug,
			Title:     full.Title,
			Path:      full.Slug,
			Markdown:  full.Content,
			WebURL: fmt.Sprintf("%s/%s/-/wikis/%s",
				strings.TrimRight(c.cfg.BaseURL, "/"), c.cfg.ProjectID, full.Slug),
			// GitLab wiki API doesn't expose per-page updated_at; leave zero.
		})
	}
	return out, nil
}

// --- HTTP helpers ---

func (c *Client) getJSON(ctx context.Context, u string, dst any) (nextPage string, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("PRIVATE-TOKEN", c.cfg.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("gitlab %d %s", resp.StatusCode, u)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return "", err
	}
	np := resp.Header.Get("X-Next-Page")
	if _, err := strconv.Atoi(np); err != nil {
		np = ""
	}
	return np, nil
}

func (c *Client) getRaw(ctx context.Context, u string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("PRIVATE-TOKEN", c.cfg.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab %d %s", resp.StatusCode, u)
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

func isMarkdown(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".md") || strings.HasSuffix(n, ".markdown")
}

func titleFromPath(p string) string {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	base = strings.TrimSuffix(base, ".md")
	base = strings.TrimSuffix(base, ".markdown")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return base
}
