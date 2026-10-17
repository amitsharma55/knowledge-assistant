package main

import (
	"context"
	"errors"
	"testing"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/servicenow"
	"github.com/example/knowledge-assistant/internal/team"
)

type stubSNClient struct {
	gotGroup string
	incs     []servicenow.Incident
}

func (s *stubSNClient) LatestOpen(_ context.Context, group string, _ int) ([]servicenow.Incident, error) {
	s.gotGroup = group
	return s.incs, nil
}

func starTeam(t *testing.T) (team.Team, *team.Registry) {
	t.Helper()
	// Groups are config-driven, so overlay them the way main.go does.
	infos := team.ApplyMetadata(team.DefaultInfos(), map[string]team.Metadata{
		"star": {ServiceNowGroup: "Star Reporting", Contact: "star@x.com"},
	})
	reg := team.NewRegistry(infos)
	tm, err := reg.Parse("star")
	if err != nil {
		t.Fatal(err)
	}
	return tm, reg
}

func TestTicketSourceResolvesGroupAndMaps(t *testing.T) {
	tm, reg := starTeam(t)
	cli := &stubSNClient{incs: []servicenow.Incident{{Number: "INC1", ShortDescription: "x", URL: "u"}}}
	src := ticketSource{reg: reg, cli: cli}
	got, err := src.LatestOpen(context.Background(), tm, 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	if cli.gotGroup != "Star Reporting" {
		t.Errorf("queried group = %q, want the star team's ServiceNowGroup", cli.gotGroup)
	}
	if len(got) != 1 || got[0].Number != "INC1" {
		t.Errorf("mapped tickets = %+v", got)
	}
}

func TestTicketSourceEmptyGroupIsNotConfigured(t *testing.T) {
	reg := team.NewRegistry([]team.Info{{Slug: "star", DisplayName: "Star"}}) // no ServiceNowGroup
	tm, _ := reg.Parse("star")
	src := ticketSource{reg: reg, cli: &stubSNClient{}}
	_, err := src.LatestOpen(context.Background(), tm, 5)
	if !errors.Is(err, rag.ErrTeamNotConfigured) {
		t.Errorf("err = %v, want ErrTeamNotConfigured", err)
	}
}
