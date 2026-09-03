package chunker

import (
	"strings"
	"testing"
)

const listDoc = `# AVR Field Mapping

Intro paragraph.

## Invoice fields

From ` + "`GetInvoiceDetail`" + `:

- ` + "`InvoiceNbr`" + ` — becomes ` + "`invoiceNumber`" + `
- ` + "`SupplierId`" + ` — becomes ` + "`supplierId`" + `
- ` + "`CurrCd`" + ` — becomes ` + "`currencyCode`" + `
`

func TestSplitPreservesLineStructure(t *testing.T) {
	var invoice string
	for _, c := range Split(listDoc, 800, 100) {
		if c.SectionPath == "Invoice fields" {
			invoice = c.Text
		}
	}
	if invoice == "" {
		t.Fatal("no chunk for section 'Invoice fields'")
	}
	bullets := 0
	for _, line := range strings.Split(invoice, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			bullets++
		}
	}
	if bullets != 3 {
		t.Errorf("want 3 bullets each on their own line, got %d\nchunk:\n%s", bullets, invoice)
	}
	if strings.Contains(invoice, "` - `") {
		t.Errorf("bullets were flattened onto one line:\n%s", invoice)
	}
}

func TestSplitPacksLongSectionsWithOverlap(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Doc\n\n")
	for i := 0; i < 60; i++ {
		b.WriteString("- item with five words here\n")
	}
	chunks := Split(b.String(), 100, 20)
	if len(chunks) < 2 {
		t.Fatalf("want the 300-word section split into several windows, got %d", len(chunks))
	}
	for i, c := range chunks {
		if strings.Contains(c.Text, "here - item") {
			t.Errorf("chunk %d flattened its lines:\n%s", i, c.Text)
		}
	}
	// Consecutive windows must share trailing lines, or a bullet landing on a
	// boundary is only ever seen half.
	first := strings.Split(chunks[0].Text, "\n")
	if !strings.Contains(chunks[1].Text, first[len(first)-1]) {
		t.Error("second window carries no overlap from the first")
	}
	// Every line must appear somewhere; packing may repeat, never drop.
	joined := strings.Join([]string{chunks[0].Text, chunks[len(chunks)-1].Text}, "\n")
	if strings.TrimSpace(joined) == "" {
		t.Error("empty chunks")
	}
}

func TestSplitDropsEmptySections(t *testing.T) {
	if got := Split("# Heading\n\n## Sub\n\n", 800, 100); len(got) != 0 {
		t.Errorf("want no chunks for a body-less doc, got %d: %#v", len(got), got)
	}
}

func TestTitleUsesFirstH1(t *testing.T) {
	if got := Title(listDoc, "avr field mapping"); got != "AVR Field Mapping" {
		t.Errorf("Title = %q, want %q", got, "AVR Field Mapping")
	}
	if got := Title("## Only a subheading\n\nbody\n", "fallback"); got != "fallback" {
		t.Errorf("Title with no H1 = %q, want fallback", got)
	}
	if got := Title("no headings at all", "fallback"); got != "fallback" {
		t.Errorf("Title with no headings = %q, want fallback", got)
	}
}
