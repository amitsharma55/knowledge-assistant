package rag

import "testing"

func TestIsGreetingMatchesPleasantries(t *testing.T) {
	yes := []string{
		"hi", "Hello", "hey", "Hi there", "hey there",
		"Good morning", "good afternoon", "good evening",
		"How are you?", "how are you doing", "how's it going?",
		"thanks", "Thank you", "thanks so much!", "thank you so much",
		"cheers", "what's up", "who are you?", "what can you do?",
	}
	for _, m := range yes {
		if !isGreeting(m) {
			t.Errorf("isGreeting(%q) = false, want true", m)
		}
	}
}

func TestIsGreetingRejectsRealQuestions(t *testing.T) {
	no := []string{
		"hi, how do I resend a failed invoice?",
		"How is the Market Conduct Annual Statement filed?",
		"hello world config for AVR",
		"thanks for the fields, what about receipts?",
		"what is the invoice sync schedule",
		"",
		"   ",
	}
	for _, m := range no {
		if isGreeting(m) {
			t.Errorf("isGreeting(%q) = true, want false", m)
		}
	}
}
