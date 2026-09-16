// Package pii detects personal / sensitive information in extracted document
// text so the upload path can hard-block high-severity content (SSN, card,
// bank, DOB) before it ever reaches a reviewer or the index. Detection is
// deterministic regex so it is testable and free; the Detector interface is
// the seam where an LLM or AWS Comprehend detector drops in later.
package pii

import (
	"regexp"
	"strings"
)

type Severity int

const (
	Low Severity = iota
	High
)

type Finding struct {
	Category string   `json:"category"`
	Severity Severity `json:"severity"`
	Excerpt  string   `json:"excerpt"` // masked snippet for the reviewer, never the raw value
	Offset   int      `json:"offset"`  // byte offset of the match in the scanned text
}

type Detector interface {
	Scan(text string) []Finding
}

// rule pairs a category+severity with a pattern. When the pattern has a
// capturing group, group 1 is the sensitive value (the surrounding keyword,
// e.g. "DOB:", is matched but not reported). luhn=true additionally requires
// the digits to pass the Luhn check, which suppresses order ids and other
// 13-16 digit runs that are not payment cards.
type rule struct {
	category string
	severity Severity
	re       *regexp.Regexp
	luhn     bool
}

type regexDetector struct{ rules []rule }

func NewRegexDetector() Detector {
	return &regexDetector{rules: []rule{
		{"ssn", High, regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), false},
		{"credit_card", High, regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`), true},
		{"dob", High, regexp.MustCompile(`(?i)(?:dob|date of birth)\D{0,10}(\d{1,2}[/-]\d{1,2}[/-]\d{2,4})`), false},
		{"bank_account", High, regexp.MustCompile(`(?i)(?:account (?:no|number|#)|routing)\D{0,10}(\d{6,17})`), false},
		{"email", Low, regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.-]+\b`), false},
		{"phone", Low, regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`), false},
	}}
}

func (d *regexDetector) Scan(text string) []Finding {
	var out []Finding
	for _, r := range d.rules {
		for _, loc := range r.re.FindAllStringSubmatchIndex(text, -1) {
			// value is the captured group if present, else the whole match.
			vs, ve := loc[0], loc[1]
			if len(loc) >= 4 && loc[2] >= 0 {
				vs, ve = loc[2], loc[3]
			}
			val := text[vs:ve]
			if r.luhn && !luhnValid(val) {
				continue
			}
			out = append(out, Finding{
				Category: r.category,
				Severity: r.severity,
				Excerpt:  mask(val),
				Offset:   vs,
			})
		}
	}
	return out
}

func HasHigh(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == High {
			return true
		}
	}
	return false
}

// mask keeps the last 4 characters and replaces the rest with a bullet so a
// reviewer sees the shape without the raw value leaking into logs or the UI.
func mask(s string) string {
	digits := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '.' {
			return -1
		}
		return r
	}, s)
	if len(digits) <= 4 {
		return strings.Repeat("•", len(digits))
	}
	return strings.Repeat("•", len(digits)-4) + digits[len(digits)-4:]
}

func luhnValid(s string) bool {
	sum, alt, n := 0, false, 0
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		n++
		d := int(c - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return n >= 13 && sum%10 == 0
}
