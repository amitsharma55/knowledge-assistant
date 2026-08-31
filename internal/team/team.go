// Package team defines the tenant boundary. A Team is a validated slug that
// can only be produced by a Registry, so a raw string from a request can
// never become a team by accident.
package team

import "errors"

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
}

// DefaultInfos is the team set for this phase.
func DefaultInfos() []Info {
	return []Info{
		{Slug: "coupa", DisplayName: "Coupa"},
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
