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
	gotGroup     string
	incs         []servicenow.Incident
	count        int
	gotWindowKey string
	gotDays      int
	created      servicenow.Incident
	gotShortDesc string
	gotPriority  string
	gotCaller    string
}

func (s *stubSNClient) LatestOpen(_ context.Context, group string, _ int) ([]servicenow.Incident, error) {
	s.gotGroup = group
	return s.incs, nil
}

func (s *stubSNClient) CountOpenedSince(_ context.Context, group, windowKey string, days int) (int, error) {
	s.gotGroup = group
	s.gotWindowKey = windowKey
	s.gotDays = days
	return s.count, nil
}

func (s *stubSNClient) Create(_ context.Context, group, shortDescription, priority, caller string) (servicenow.Incident, error) {
	s.gotGroup = group
	s.gotShortDesc = shortDescription
	s.gotPriority = priority
	s.gotCaller = caller
	return s.created, nil
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

func TestTicketSourceMapsOpenedOn(t *testing.T) {
	tm, reg := starTeam(t)
	cli := &stubSNClient{incs: []servicenow.Incident{{Number: "INC1", OpenedOn: "2026-10-01 09:00:00"}}}
	src := ticketSource{reg: reg, cli: cli}
	got, err := src.LatestOpen(context.Background(), tm, 5)
	if err != nil {
		t.Fatalf("LatestOpen: %v", err)
	}
	if got[0].OpenedOn != "2026-10-01 09:00:00" {
		t.Errorf("OpenedOn not mapped: %+v", got[0])
	}
}

func TestTicketSourceCountResolvesGroupAndWindow(t *testing.T) {
	tm, reg := starTeam(t)
	cli := &stubSNClient{count: 4}
	src := ticketSource{reg: reg, cli: cli}
	n, err := src.CountOpened(context.Background(), tm, rag.CountWindow{Key: "days", Days: 7, Label: "the last 7 days"})
	if err != nil {
		t.Fatalf("CountOpened: %v", err)
	}
	if n != 4 {
		t.Errorf("count = %d, want 4", n)
	}
	if cli.gotGroup != "Star Reporting" || cli.gotWindowKey != "days" || cli.gotDays != 7 {
		t.Errorf("client got group=%q key=%q days=%d, want Star Reporting/days/7", cli.gotGroup, cli.gotWindowKey, cli.gotDays)
	}
}

func TestTicketSourceCreateResolvesGroupAndMaps(t *testing.T) {
	tm, reg := starTeam(t)
	cli := &stubSNClient{created: servicenow.Incident{Number: "INC9", URL: "u9"}}
	src := ticketSource{reg: reg, cli: cli}
	got, err := src.Create(context.Background(), tm, rag.TicketDraft{ShortDescription: "email down", Priority: "2"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cli.gotGroup != "Star Reporting" || cli.gotShortDesc != "email down" || cli.gotPriority != "2" {
		t.Errorf("client got group=%q desc=%q prio=%q", cli.gotGroup, cli.gotShortDesc, cli.gotPriority)
	}
	if cli.gotCaller != "" {
		t.Errorf("caller = %q, want empty for now", cli.gotCaller)
	}
	if got.Number != "INC9" || got.URL != "u9" {
		t.Errorf("mapped ticket = %+v", got)
	}
}

func TestTicketSourceCreateEmptyGroupIsNotConfigured(t *testing.T) {
	reg := team.NewRegistry([]team.Info{{Slug: "star", DisplayName: "Star"}})
	tm, _ := reg.Parse("star")
	src := ticketSource{reg: reg, cli: &stubSNClient{}}
	if _, err := src.Create(context.Background(), tm, rag.TicketDraft{ShortDescription: "x", Priority: "3"}); !errors.Is(err, rag.ErrTeamNotConfigured) {
		t.Errorf("err = %v, want ErrTeamNotConfigured", err)
	}
}

func TestTicketSourceCountEmptyGroupIsNotConfigured(t *testing.T) {
	reg := team.NewRegistry([]team.Info{{Slug: "star", DisplayName: "Star"}})
	tm, _ := reg.Parse("star")
	src := ticketSource{reg: reg, cli: &stubSNClient{}}
	if _, err := src.CountOpened(context.Background(), tm, rag.CountWindow{Key: "days", Days: 7}); !errors.Is(err, rag.ErrTeamNotConfigured) {
		t.Errorf("err = %v, want ErrTeamNotConfigured", err)
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
