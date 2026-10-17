package main

import (
	"context"

	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/servicenow"
	"github.com/example/knowledge-assistant/internal/team"
)

// snClient is the slice of *servicenow.Client the adapter needs, named so tests
// can stub it.
type snClient interface {
	LatestOpen(ctx context.Context, group string, limit int) ([]servicenow.Incident, error)
	CountOpenedSince(ctx context.Context, group, windowKey string, days int) (int, error)
}

// ticketSource adapts a ServiceNow client to rag.TicketSource: it resolves the
// team's assignment group from the registry and maps incidents to rag.Ticket.
// This is the only place servicenow and team config meet rag.
type ticketSource struct {
	reg *team.Registry
	cli snClient
}

func (s ticketSource) LatestOpen(ctx context.Context, t team.Team, limit int) ([]rag.Ticket, error) {
	group := s.reg.Info(t).ServiceNowGroup
	if group == "" {
		return nil, rag.ErrTeamNotConfigured
	}
	incs, err := s.cli.LatestOpen(ctx, group, limit)
	if err != nil {
		return nil, err
	}
	out := make([]rag.Ticket, len(incs))
	for i, in := range incs {
		out[i] = rag.Ticket{
			Number:           in.Number,
			ShortDescription: in.ShortDescription,
			State:            in.State,
			Priority:         in.Priority,
			UpdatedOn:        in.UpdatedOn,
			OpenedOn:         in.OpenedOn,
			URL:              in.URL,
		}
	}
	return out, nil
}

func (s ticketSource) CountOpened(ctx context.Context, t team.Team, w rag.CountWindow) (int, error) {
	group := s.reg.Info(t).ServiceNowGroup
	if group == "" {
		return 0, rag.ErrTeamNotConfigured
	}
	return s.cli.CountOpenedSince(ctx, group, w.Key, w.Days)
}
