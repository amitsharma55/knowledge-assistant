package review

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore_EnqueueListGet(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	id, err := s.Enqueue(ctx, Item{Team: "coupa", Filename: "a.pdf", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Pending || got.Filename != "a.pdf" {
		t.Fatalf("unexpected item: %+v", got)
	}
	list, err := s.List(ctx, "coupa")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
}

func TestMemoryStore_ListIsTeamScoped(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_, _ = s.Enqueue(ctx, Item{Team: "coupa", Filename: "a.pdf"})
	_, _ = s.Enqueue(ctx, Item{Team: "star", Filename: "b.pdf"})
	list, _ := s.List(ctx, "coupa")
	if len(list) != 1 || list[0].Team != "coupa" {
		t.Fatalf("team A must not see team B items: %+v", list)
	}
}

func TestMemoryStore_SetStatusRemovesFromPendingList(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	id, _ := s.Enqueue(ctx, Item{Team: "coupa", Filename: "a.pdf"})
	if err := s.SetStatus(ctx, id, Approved); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ctx, "coupa")
	if len(list) != 0 {
		t.Fatalf("approved item still in pending list: %+v", list)
	}
	got, _ := s.Get(ctx, id)
	if got.Status != Approved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
}

func TestMemoryStore_SetStatusMissing(t *testing.T) {
	s := NewMemoryStore()
	if err := s.SetStatus(context.Background(), "nope", Approved); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
