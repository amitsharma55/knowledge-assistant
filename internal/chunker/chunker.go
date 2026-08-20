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
func Split(md string, targetTokens, overlap int) []Chunk {
	sections := splitByHeading(md)
	var out []Chunk
	for _, s := range sections {
		words := strings.Fields(s.body)
		if len(words) == 0 {
			continue
		}
		step := targetTokens - overlap
		if step <= 0 {
			step = targetTokens
		}
		for i := 0; i < len(words); i += step {
			end := i + targetTokens
			if end > len(words) {
				end = len(words)
			}
			out = append(out, Chunk{
				SectionPath: s.heading,
				Text:        strings.Join(words[i:end], " "),
			})
			if end == len(words) {
				break
			}
		}
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
