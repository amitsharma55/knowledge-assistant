package extract_test

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/example/knowledge-assistant/internal/extract"
	"github.com/xuri/excelize/v2"
)

func TestFromBytes_XLSX(t *testing.T) {
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "Team")
	_ = f.SetCellValue("Sheet1", "B1", "Owner")
	_ = f.SetCellValue("Sheet1", "A2", "Coupa")
	_ = f.SetCellValue("Sheet1", "B2", "Alex")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := extract.FromBytes("report.xlsx", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Coupa") || !strings.Contains(got, "Alex") {
		t.Fatalf("xlsx text missing cell values: %q", got)
	}
}

func TestFromBytes_DOCX(t *testing.T) {
	data := makeDocx(t, "Invoice resend runbook", "Retry from the Coupa queue.")
	got, err := extract.FromBytes("runbook.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Invoice resend runbook") || !strings.Contains(got, "Coupa queue") {
		t.Fatalf("docx text missing paragraphs: %q", got)
	}
}

func TestSupported_OfficeFormats(t *testing.T) {
	for _, n := range []string{"a.docx", "b.xlsx"} {
		if !extract.Supported(n) {
			t.Errorf("Supported(%q) = false, want true", n)
		}
	}
}

// makeDocx builds the minimal OOXML zip that FromBytes needs: a
// word/document.xml with each argument as its own <w:p><w:t> paragraph.
func makeDocx(t *testing.T, paras ...string) []byte {
	t.Helper()
	var body strings.Builder
	for _, p := range paras {
		body.WriteString("<w:p><w:r><w:t>" + p + "</w:t></w:r></w:p>")
	}
	doc := `<?xml version="1.0"?><w:document xmlns:w="x"><w:body>` + body.String() + `</w:body></w:document>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
