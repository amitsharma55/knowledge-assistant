package eval

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/example/knowledge-assistant/internal/rag"
)

// A golden entry is one query plus the chunks that should answer it. Categories
// bucket queries by failure mode so metrics report per-bucket: the near-twin
// regression and the literal-token lift must never average each other out.
const (
	CategoryLiteralToken = "literal_token" // answer hinges on an exact token/ID (AVR-TIMEOUT)
	CategoryNearTwin     = "near_twin"     // a sibling doc shares the query's vocabulary (Louisiana vs Texas)
	CategoryNormal       = "normal"        // neither; an ordinary paraphrasable question
)

var categories = map[string]bool{
	CategoryLiteralToken: true, CategoryNearTwin: true, CategoryNormal: true,
}

// Label marks a page (optionally one section of it) as relevant to a query.
//
// It keys on PageID + Section rather than the chunk's hash id on purpose. Hash
// ids are sha1(team,page,section,text) in production and team:page:index in
// fixtures mode, and both churn whenever the chunker changes -- a gold set keyed
// by them would silently rot on exactly the kind of change this harness is meant
// to survive. PageID is the fixture filename and Section is the literal heading:
// both are stable and authorable by hand, and PageID alone is the discriminator
// a near-twin turns on (louisiana-processing-rules vs texas-processing-rules).
type Label struct {
	PageID  string  `json:"pageId"`
	Section string  `json:"section,omitempty"` // "" matches any section of the page
	Grade   float64 `json:"grade"`             // >0 relevant; higher = more relevant, for NDCG
}

func (l Label) matches(c rag.Chunk) bool {
	return c.PageID == l.PageID && (l.Section == "" || c.SectionPath == l.Section)
}

// Entry is one golden query. Relevant must be non-empty: a query with no labels
// would score a meaningless 0/0 and quietly drag the average down.
type Entry struct {
	ID       string  `json:"id"`
	Query    string  `json:"query"`
	Team     string  `json:"team"`
	Category string  `json:"category"`
	Relevant []Label `json:"relevant"`
}

// Load reads and validates a golden set. Validation is strict because a typo'd
// team or category produces plausible-looking numbers rather than an error, and
// a silently empty gold set is worse than a loud failure.
func Load(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read golden set: %w", err)
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("eval: decode golden set %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("eval: golden set %s is empty", path)
	}
	seen := map[string]bool{}
	for i, e := range entries {
		switch {
		case e.ID == "":
			return nil, fmt.Errorf("eval: %s: entry %d has no id", path, i)
		case seen[e.ID]:
			return nil, fmt.Errorf("eval: %s: duplicate id %q", path, e.ID)
		case e.Query == "":
			return nil, fmt.Errorf("eval: %s: %q has no query", path, e.ID)
		case e.Team == "":
			return nil, fmt.Errorf("eval: %s: %q has no team", path, e.ID)
		case !categories[e.Category]:
			return nil, fmt.Errorf("eval: %s: %q has unknown category %q (want literal_token, near_twin or normal)", path, e.ID, e.Category)
		case len(e.Relevant) == 0:
			return nil, fmt.Errorf("eval: %s: %q has no relevant labels", path, e.ID)
		}
		for _, l := range e.Relevant {
			if l.PageID == "" || l.Grade <= 0 {
				return nil, fmt.Errorf("eval: %s: %q has a label with empty pageId or non-positive grade", path, e.ID)
			}
		}
		seen[e.ID] = true
	}
	return entries, nil
}

// CountByCategory tallies entries per category, for the harness header.
func CountByCategory(entries []Entry) map[string]int {
	out := map[string]int{}
	for _, e := range entries {
		out[e.Category]++
	}
	return out
}
