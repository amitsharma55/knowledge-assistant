// Package extract turns uploaded bytes into plain text suitable for chunking.
// Supports PDF (text-only), Markdown, plain text. Scanned/image-only PDFs
// return the empty string — OCR is a separate concern.
package extract

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

var ErrUnsupported = errors.New("unsupported file type")

// FromBytes dispatches on the filename extension.
func FromBytes(filename string, data []byte) (string, error) {
	name := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(name, ".pdf"):
		return fromPDF(data)
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
