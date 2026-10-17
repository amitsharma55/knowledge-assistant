package team

import "testing"

func TestParseMetadata(t *testing.T) {
	m, err := ParseMetadata(`{"star":{"serviceNowGroup":"Star Support","contact":"star@x.com"}}`)
	if err != nil {
		t.Fatalf("ParseMetadata: %v", err)
	}
	if m["star"].ServiceNowGroup != "Star Support" || m["star"].Contact != "star@x.com" {
		t.Errorf("parsed star = %+v", m["star"])
	}
	empty, err := ParseMetadata("")
	if err != nil || len(empty) != 0 {
		t.Errorf("ParseMetadata(\"\") = %v, %v; want empty map, nil", empty, err)
	}
	if _, err := ParseMetadata("{not json"); err == nil {
		t.Error("ParseMetadata on malformed JSON = nil error, want error")
	}
}

func TestApplyMetadata(t *testing.T) {
	infos := []Info{{Slug: "star", DisplayName: "Star"}, {Slug: "hr", DisplayName: "HR"}}
	out := ApplyMetadata(infos, map[string]Metadata{
		"star":    {ServiceNowGroup: "Star Support", Contact: "s@x.com"},
		"unknown": {ServiceNowGroup: "Nope"},
	})
	bySlug := map[string]Info{}
	for _, i := range out {
		bySlug[i.Slug] = i
	}
	if bySlug["star"].ServiceNowGroup != "Star Support" || bySlug["star"].Contact != "s@x.com" {
		t.Errorf("star not overlaid: %+v", bySlug["star"])
	}
	if bySlug["hr"].ServiceNowGroup != "" {
		t.Errorf("hr should have no group, got %q", bySlug["hr"].ServiceNowGroup)
	}
	if len(out) != 2 {
		t.Errorf("unknown slug leaked a team: got %d infos, want 2", len(out))
	}
	// Input must not be mutated.
	if infos[0].ServiceNowGroup != "" {
		t.Error("ApplyMetadata mutated its input slice")
	}
}
