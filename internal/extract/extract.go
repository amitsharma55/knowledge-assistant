// Package extract turns uploaded bytes into plain text suitable for chunking.
// Supports PDF (text-only), Word (.docx), Excel (.xlsx), Markdown, plain text.
// Scanned/image-only PDFs return the empty string — OCR is a separate concern.
package extract

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

var ErrUnsupported = errors.New("unsupported file type")

// FromBytes dispatches on the filename extension.
func FromBytes(filename string, data []byte) (string, error) {
	name := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(name, ".pdf"):
		return fromPDF(data)
	case strings.HasSuffix(name, ".docx"):
		return fromDOCX(data)
	case strings.HasSuffix(name, ".xlsx"):
		return fromXLSX(data)
	case strings.HasSuffix(name, ".md"),
		strings.HasSuffix(name, ".markdown"),
		strings.HasSuffix(name, ".txt"),
		strings.HasSuffix(name, ".text"):
		return string(data), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, filename)
	}
}

// Supported reports whether FromBytes can read the file named name.
func Supported(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".pdf") ||
		strings.HasSuffix(n, ".docx") ||
		strings.HasSuffix(n, ".xlsx") ||
		strings.HasSuffix(n, ".md") ||
		strings.HasSuffix(n, ".markdown") ||
		strings.HasSuffix(n, ".txt") ||
		strings.HasSuffix(n, ".text")
}

func fromPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf open: %w", err)
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		txt, err := p.GetPlainText(nil)
		if err != nil {
			continue // skip unparseable pages, keep going
		}
		b.WriteString(txt)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// wTextRe pulls the text out of every <w:t> run. We deliberately avoid a full
// OOXML parse: a regex over document.xml is enough to recover readable prose
// for chunking, and adds no dependency.
var wTextRe = regexp.MustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)

func fromDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("extract: docx open: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("extract: docx read: %w", err)
		}
		xmlBytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("extract: docx read: %w", err)
		}
		var b strings.Builder
		for _, m := range wTextRe.FindAllSubmatch(xmlBytes, -1) {
			b.Write(m[1])
			b.WriteByte(' ')
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("extract: docx: no word/document.xml")
}

// fromXLSX flattens every sheet to "header: value" lines so a spreadsheet
// becomes retrievable prose. Tables chunk poorly (see the upload-review spec's
// Risks); this is the pragmatic demo behavior, not high-quality tabular handling.
func fromXLSX(data []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("extract: xlsx open: %w", err)
	}
	defer f.Close()
	var b strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("extract: xlsx rows: %w", err)
		}
		if len(rows) == 0 {
			continue
		}
		headers := rows[0]
		for _, row := range rows[1:] {
			for i, cell := range row {
				if cell == "" {
					continue
				}
				h := ""
				if i < len(headers) {
					h = headers[i]
				}
				b.WriteString(h + ": " + cell + "\n")
			}
		}
	}
	return b.String(), nil
}
