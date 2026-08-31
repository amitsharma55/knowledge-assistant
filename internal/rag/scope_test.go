package rag

import (
	"testing"

	"github.com/example/knowledge-assistant/internal/team"
)

func TestScopeCarriesActiveTeamAndOthers(t *testing.T) {
	r := team.NewRegistry(team.DefaultInfos())
	star, _ := r.Parse("star")
	hr, _ := r.Parse("hr")

	s := NewScope(star, []team.Team{star, hr}, []string{"everyone"})

	if s.Team() != star {
		t.Errorf("Team() = %q, want star", s.Team().Slug())
	}
	others := s.Others()
	if len(others) != 1 || others[0] != hr {
		t.Errorf("Others() = %v, want [hr]", others)
	}
	if s.IsZero() {
		t.Error("IsZero() = true for a constructed scope, want false")
	}
}

func TestZeroScopeIsRecognisable(t *testing.T) {
	var s Scope
	if !s.IsZero() {
		t.Fatal("zero Scope reports IsZero() = false; retrievers rely on this to refuse")
	}
}
