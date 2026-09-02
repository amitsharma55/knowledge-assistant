package chunker

import (
	"strings"
	"testing"
)

func TestEmbedTextCarriesTitleAndSection(t *testing.T) {
	got := EmbedText("AVR SOAP Service", "Operations",
		"The middleware uses four of the operations the WSDL exposes.")

	// The words a person searches with -- the integration's name and the
	// section's name -- live in the heading, which Split keeps as metadata.
	// Without them in the embedded text, a section called "Operations" under
	// "AVR SOAP Service" contains neither "AVR" nor "SOAP".
	for _, want := range []string{"AVR SOAP Service", "Operations", "WSDL"} {
		if !strings.Contains(got, want) {
			t.Errorf("EmbedText() = %q, missing %q", got, want)
		}
	}
	if !strings.HasPrefix(got, "AVR SOAP Service") {
		t.Errorf("EmbedText() = %q, want the page title first", got)
	}
}

func TestEmbedTextOmitsMissingParts(t *testing.T) {
	cases := []struct {
		name                       string
		title, section, body, want string
	}{
		{
			name:  "no section (document has no headings)",
			title: "Leave Policy", section: "", body: "Employees accrue 25 days.",
			want: "Leave Policy\n\nEmployees accrue 25 days.",
		},
		{
			name: "no title", title: "", section: "Jobs", body: "Runs hourly.",
			want: "Jobs\n\nRuns hourly.",
		},
		{
			name: "neither", title: "", section: "", body: "Bare text.",
			want: "Bare text.",
		},
		{
			name: "both", title: "AVR Runbook", section: "Error codes", body: "AVR-TIMEOUT.",
			want: "AVR Runbook — Error codes\n\nAVR-TIMEOUT.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EmbedText(c.title, c.section, c.body); got != c.want {
				t.Errorf("EmbedText(%q, %q, %q) = %q, want %q",
					c.title, c.section, c.body, got, c.want)
			}
		})
	}
}

// The heading must not be repeated when the section body already opens with
// it, which happens for a document whose H1 is its only heading.
func TestEmbedTextDoesNotDuplicateHeading(t *testing.T) {
	got := EmbedText("AVR Runbook", "AVR Runbook", "What to do when Coupa reports failures.")
	if strings.Count(got, "AVR Runbook") != 1 {
		t.Errorf("EmbedText() = %q, want the title once when title and section match", got)
	}
}
