package chunker

import (
	"regexp"
	"strings"
)

// HTMLToMarkdown is a small, dependency-free converter sufficient for
// Confluence storage-format HTML. For production, swap in a real converter
// (e.g. github.com/JohannesKaufmann/html-to-markdown) — this exists so the
// prototype has no external deps.
func HTMLToMarkdown(html string) string {
	s := html
	for _, r := range headingReplacers {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	s = liRE.ReplaceAllString(s, "- $1\n")
	s = pRE.ReplaceAllString(s, "$1\n\n")
	s = brRE.ReplaceAllString(s, "\n")
	s = codeRE.ReplaceAllString(s, "`$1`")
	s = tagRE.ReplaceAllString(s, "")
	s = wsRE.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

type replacer struct {
	re   *regexp.Regexp
	repl string
}

var (
	headingReplacers = []replacer{
		{regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`), "\n# $1\n"},
		{regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2>`), "\n## $1\n"},
		{regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>`), "\n### $1\n"},
		{regexp.MustCompile(`(?is)<h4[^>]*>(.*?)</h4>`), "\n#### $1\n"},
	}
	liRE   = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	pRE    = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	brRE   = regexp.MustCompile(`(?i)<br\s*/?>`)
	codeRE = regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`)
	tagRE  = regexp.MustCompile(`(?is)<[^>]+>`)
	wsRE   = regexp.MustCompile(`\n{3,}`)
)
