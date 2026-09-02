package chunker

import "strings"

// EmbedText returns the text that should be embedded for a chunk: the page
// title and section heading, followed by the section body.
//
// Split keeps a section's heading in SectionPath and the body in Text, so the
// body alone is what would otherwise be embedded -- and a section called
// "Operations" under a page titled "AVR SOAP Service" then contains neither
// "AVR" nor "SOAP". Those are exactly the words someone searches with, so
// such a chunk ranks far below less relevant ones, and no amount of rewording
// the body fixes it. Prepending the heading puts them back.
//
// Only the embedded text changes. The stored chunk text stays the clean body,
// so the context panel and the prompt are unaffected.
func EmbedText(pageTitle, sectionPath, body string) string {
	heading := strings.TrimSpace(pageTitle)
	section := strings.TrimSpace(sectionPath)

	// A document whose only heading is its H1 yields a section whose path
	// equals the page title. Repeating it adds nothing and skews the
	// embedding toward the title.
	if section != "" && !strings.EqualFold(section, heading) {
		if heading == "" {
			heading = section
		} else {
			heading += " — " + section
		}
	}
	if heading == "" {
		return body
	}
	return heading + "\n\n" + body
}
