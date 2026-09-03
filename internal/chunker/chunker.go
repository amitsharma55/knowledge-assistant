package chunker

import (
	"regexp"
	"strings"
)

type Chunk struct {
	SectionPath string
	Text        string
}

// Split breaks markdown into chunks by top-level headings and then packs
// into ~targetTokens windows (word-approximated) with `overlap` words
// carried between windows.
//
// Windows break only at line boundaries. An earlier version packed by word
// (strings.Fields then Join(" ")), which collapsed every newline: a section
// of twenty bullet points reached the model as one run-on paragraph, and the
// model reproduced it as one, because a flattened list is genuinely all it
// was given. Line structure is part of the content, so it survives here.
func Split(md string, targetTokens, overlap int) []Chunk {
	sections := splitByHeading(md)
	var out []Chunk
	for _, s := range sections {
		for _, body := range packLines(s.body, targetTokens, overlap) {
			out = append(out, Chunk{SectionPath: s.heading, Text: body})
		}
	}
	return out
}

// packLines groups body's lines into windows of roughly targetTokens words,
// never splitting a line. Consecutive windows repeat the trailing lines
// making up about `overlap` words, so a list item cut by a window boundary
// still appears whole in one of them.
func packLines(body string, targetTokens, overlap int) []string {
	lines := strings.Split(body, "\n")
	counts := make([]int, len(lines))
	total := 0
	for i, l := range lines {
		counts[i] = len(strings.Fields(l))
		total += counts[i]
	}
	if total == 0 {
		return nil
	}
	if total <= targetTokens {
		return []string{strings.TrimSpace(body)}
	}

	var out []string
	for start := 0; start < len(lines); {
		words, end := 0, start
		for end < len(lines) && words < targetTokens {
			words += counts[end]
			end++
		}
		if text := strings.TrimSpace(strings.Join(lines[start:end], "\n")); text != "" {
			out = append(out, text)
		}
		if end >= len(lines) {
			break
		}
		// Walk back over whole lines until ~overlap words are carried into
		// the next window. The start+1 floor guarantees forward progress
		// even when one line is longer than the overlap budget.
		back, acc := end, 0
		for back > start+1 && acc < overlap {
			back--
			acc += counts[back]
		}
		start = back
	}
	return out
}

type section struct {
	heading string
	body    string
}

var headingRE = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

func splitByHeading(md string) []section {
	locs := headingRE.FindAllStringSubmatchIndex(md, -1)
	if len(locs) == 0 {
		return []section{{heading: "", body: md}}
	}
	var out []section
	for i, loc := range locs {
		heading := md[loc[4]:loc[5]]
		start := loc[1]
		end := len(md)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, section{heading: heading, body: strings.TrimSpace(md[start:end])})
	}
	return out
}

// Title returns the document's first level-1 heading, which is the title a
// reader sees and the one worth showing in a citation. Ingest sources that
// carry no title of their own (a directory of .md files) otherwise fall back
// to the filename slug, and cite "avr field mapping" for a page whose actual
// heading reads "AVR Field Mapping".
func Title(md, fallback string) string {
	for _, loc := range headingRE.FindAllStringSubmatchIndex(md, -1) {
		if loc[3]-loc[2] != 1 { // not an H1
			continue
		}
		if h := strings.TrimSpace(md[loc[4]:loc[5]]); h != "" {
			return h
		}
	}
	return fallback
}
