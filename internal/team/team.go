// Package team defines the tenant boundary. A Team is a validated slug that
// can only be produced by a Registry, so a raw string from a request can
// never become a team by accident.
package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnknownTeam = errors.New("team: unknown team")
	ErrNotAllowed  = errors.New("team: caller is not a member of this team")
)

// Team is an opaque, validated team identifier. The zero value is invalid.
type Team struct{ slug string }

func (t Team) Slug() string { return t.slug }
func (t Team) IsZero() bool { return t.slug == "" }

// Info is the configured description of one team.
type Info struct {
	Slug        string
	DisplayName string
	SiteID      string // SharePoint site; unused until the ingestion plan
	// ServiceNowGroup is the incident assignment_group ("workgroup") queried
	// for this team's open tickets. Empty disables ticket lookup for the team.
	// Set from config (KA_TEAM_METADATA), not a literal, so a workgroup can
	// change without a rebuild.
	ServiceNowGroup string
	// Contact is the owning/on-call contact. Config-driven; stored for display,
	// not yet surfaced in answers.
	Contact string
}

// Metadata is the per-team ticket configuration overlaid onto Info at startup.
// It is config-driven (KA_TEAM_METADATA) so a workgroup or contact can change
// without rebuilding the binary.
type Metadata struct {
	ServiceNowGroup string `json:"serviceNowGroup"`
	Contact         string `json:"contact"`
}

// ParseMetadata reads a JSON object of team slug -> Metadata. An empty string is
// a valid "nothing configured" input (empty map, no error); malformed JSON is an
// error so a typo fails loudly at startup rather than silently disabling tickets.
func ParseMetadata(raw string) (map[string]Metadata, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]Metadata{}, nil
	}
	var m map[string]Metadata
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("team: parse KA_TEAM_METADATA: %w (want a JSON object of slug -> {serviceNowGroup, contact})", err)
	}
	return m, nil
}

// ApplyMetadata returns a copy of infos with each team's ServiceNowGroup and
// Contact filled from meta by slug. Slugs in meta with no matching team are
// ignored; the input slice is not mutated.
func ApplyMetadata(infos []Info, meta map[string]Metadata) []Info {
	out := make([]Info, len(infos))
	copy(out, infos)
	for i := range out {
		if md, ok := meta[out[i].Slug]; ok {
			out[i].ServiceNowGroup = md.ServiceNowGroup
			out[i].Contact = md.Contact
		}
	}
	return out
}

// DefaultInfos is the team set for this phase.
func DefaultInfos() []Info {
	return []Info{
		{Slug: "coupa", DisplayName: "Coupa AWS Middleware"},
		{Slug: "star", DisplayName: "Star"},
		{Slug: "hr", DisplayName: "HR"},
	}
}

type Registry struct {
	order  []Info
	bySlug map[string]Info
}

func NewRegistry(infos []Info) *Registry {
	r := &Registry{order: infos, bySlug: make(map[string]Info, len(infos))}
	for _, i := range infos {
		r.bySlug[i.Slug] = i
	}
	return r
}

// Parse turns an untrusted string into a Team, or fails.
func (r *Registry) Parse(slug string) (Team, error) {
	if _, ok := r.bySlug[slug]; !ok {
		return Team{}, ErrUnknownTeam
	}
	return Team{slug: slug}, nil
}

func (r *Registry) All() []Info { return r.order }

func (r *Registry) Info(t Team) Info { return r.bySlug[t.slug] }

// Authorize reports whether requested is in allowed. This is the only
// authorisation decision in the retrieval path; keep it that way.
func Authorize(allowed []Team, requested Team) error {
	if requested.IsZero() {
		return ErrNotAllowed
	}
	for _, a := range allowed {
		if a == requested {
			return nil
		}
	}
	return ErrNotAllowed
}
