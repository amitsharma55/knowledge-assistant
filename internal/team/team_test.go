package team

import "testing"

func TestParseAcceptsOnlyRegisteredSlugs(t *testing.T) {
	r := NewRegistry(DefaultInfos())
	for _, slug := range []string{"coupa", "star", "hr"} {
		got, err := r.Parse(slug)
		if err != nil {
			t.Fatalf("Parse(%q) returned error %v, want nil", slug, err)
		}
		if got.Slug() != slug {
			t.Errorf("Parse(%q).Slug() = %q, want %q", slug, got.Slug(), slug)
		}
	}
	for _, slug := range []string{"", "COUPA", "finance", "coupa ", "../hr"} {
		if _, err := r.Parse(slug); err == nil {
			t.Errorf("Parse(%q) returned nil error, want ErrUnknownTeam", slug)
		}
	}
}

func TestZeroTeamIsInvalid(t *testing.T) {
	var zero Team
	if !zero.IsZero() {
		t.Fatal("zero Team reports IsZero() = false, want true")
	}
	if zero.Slug() != "" {
		t.Errorf("zero Team Slug() = %q, want empty", zero.Slug())
	}
}

func TestAuthorize(t *testing.T) {
	r := NewRegistry(DefaultInfos())
	coupa, _ := r.Parse("coupa")
	star, _ := r.Parse("star")
	hr, _ := r.Parse("hr")

	if err := Authorize([]Team{star, hr}, hr); err != nil {
		t.Errorf("Authorize(member of hr, hr) = %v, want nil", err)
	}
	if err := Authorize([]Team{star, hr}, coupa); err == nil {
		t.Error("Authorize(member of star+hr, coupa) = nil, want ErrNotAllowed")
	}
	if err := Authorize(nil, coupa); err == nil {
		t.Error("Authorize(no teams, coupa) = nil, want ErrNotAllowed")
	}
	var zero Team
	if err := Authorize([]Team{coupa}, zero); err == nil {
		t.Error("Authorize(coupa, zero team) = nil, want ErrNotAllowed")
	}
}

func TestDefaultInfosDisplayNames(t *testing.T) {
	want := map[string]string{
		"coupa": "Coupa AWS Middleware",
		"star":  "Star",
		"hr":    "HR",
	}
	infos := DefaultInfos()
	if len(infos) != len(want) {
		t.Fatalf("DefaultInfos() returned %d teams, want %d", len(infos), len(want))
	}
	for _, info := range infos {
		w, ok := want[info.Slug]
		if !ok {
			t.Errorf("unexpected team slug %q", info.Slug)
			continue
		}
		if info.DisplayName != w {
			t.Errorf("DisplayName for %q = %q, want %q", info.Slug, info.DisplayName, w)
		}
	}
}
