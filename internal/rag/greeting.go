package rag

import (
	"strings"
	"unicode"
)

// greetings is the closed set of whole-message pleasantries that get a warm
// reply instead of a document search.
//
// We match the *entire* normalized message against this set rather than
// looking for a threshold in the retrieval scores, because on this embedder
// the scores don't separate the two: "How are you?" tops out around 0.72 and
// an answerable question like "How is the Market Conduct Annual Statement
// filed?" around 0.78, so any floor that catches the greeting also refuses
// real questions. Whole-message matching is what keeps the gate fail-safe --
// "hi, how do I resend an invoice?" is not in the set, so it falls through to
// normal retrieval and is never mistaken for small talk.
//
// Entries are stored in normalized form (see normalizeGreeting): lowercase,
// letters and single spaces only. So "how's it going" is keyed as
// "hows it going" and "thank you!" as "thank you".
var greetings = map[string]struct{}{
	"hi": {}, "hello": {}, "hey": {}, "heya": {}, "hiya": {}, "yo": {},
	"howdy": {}, "greetings": {}, "sup": {}, "morning": {},
	"hi there": {}, "hey there": {}, "hello there": {},
	"good morning": {}, "good afternoon": {}, "good evening": {}, "good day": {},
	"how are you": {}, "how are you doing": {}, "how are u": {}, "how r u": {},
	"hows it going": {}, "how is it going": {}, "hows things": {},
	"whats up": {}, "what is up": {}, "how do you do": {},
	"thanks": {}, "thank you": {}, "thanks a lot": {}, "thanks so much": {},
	"thank you so much": {}, "many thanks": {}, "thanks very much": {}, "cheers": {},
	"nice to meet you": {}, "good to meet you": {},
	"who are you": {}, "what can you do": {}, "what do you do": {},
}

// isGreeting reports whether the message is nothing but a greeting or
// pleasantry, in which case the orchestrator answers conversationally and
// skips retrieval entirely.
func isGreeting(msg string) bool {
	n := normalizeGreeting(msg)
	if n == "" {
		return false
	}
	_, ok := greetings[n]
	return ok
}

// normalizeGreeting lowercases the message and keeps only letters separated by
// single spaces, dropping punctuation, digits and apostrophes so that casing,
// trailing "?"/"!" and "how's" vs "hows" all collapse to one key.
func normalizeGreeting(msg string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(strings.TrimSpace(msg)) {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}
