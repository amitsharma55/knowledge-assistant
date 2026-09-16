package pii

import "testing"

func TestRegexDetector_HighSeverity(t *testing.T) {
	d := NewRegexDetector()
	cases := []struct {
		name, text, cat string
	}{
		{"ssn", "employee ssn 123-45-6789 on file", "ssn"},
		{"visa card", "card 4111 1111 1111 1111 charged", "credit_card"},
		{"dob", "DOB: 04/12/1985", "dob"},
		{"bank", "account number 000123456789 at bank", "bank_account"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := d.Scan(c.text)
			if !HasHigh(fs) {
				t.Fatalf("expected a high-severity finding, got %+v", fs)
			}
			found := false
			for _, f := range fs {
				if f.Category == c.cat {
					found = true
					if f.Severity != High {
						t.Errorf("%s: severity = %v, want High", c.cat, f.Severity)
					}
				}
			}
			if !found {
				t.Errorf("category %q not found in %+v", c.cat, fs)
			}
		})
	}
}

func TestRegexDetector_LowSeverity(t *testing.T) {
	d := NewRegexDetector()
	fs := d.Scan("reach me at jane@example.com or (415) 555-0142")
	if HasHigh(fs) {
		t.Fatalf("email/phone must not be high severity: %+v", fs)
	}
	got := map[string]bool{}
	for _, f := range fs {
		got[f.Category] = true
	}
	if !got["email"] || !got["phone"] {
		t.Errorf("want email+phone, got %+v", fs)
	}
}

func TestRegexDetector_LuhnRejectsInvalidCard(t *testing.T) {
	d := NewRegexDetector()
	// 16 digits that fail the Luhn check must not be reported as a card.
	fs := d.Scan("order id 1234 5678 9012 3456 shipped")
	for _, f := range fs {
		if f.Category == "credit_card" {
			t.Fatalf("Luhn-invalid number reported as card: %+v", f)
		}
	}
}

func TestRegexDetector_Clean(t *testing.T) {
	d := NewRegexDetector()
	if fs := d.Scan("The invoice resend runbook lives in Coupa."); len(fs) != 0 {
		t.Fatalf("clean text produced findings: %+v", fs)
	}
}

func TestFinding_ExcerptIsMasked(t *testing.T) {
	d := NewRegexDetector()
	fs := d.Scan("ssn 123-45-6789")
	for _, f := range fs {
		if f.Category == "ssn" && (f.Excerpt == "" || indexOf(f.Excerpt, "123-45-6789") >= 0) {
			t.Fatalf("excerpt must be masked, got %q", f.Excerpt)
		}
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
