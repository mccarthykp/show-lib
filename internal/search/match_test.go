package search

import "testing"

func TestScore(t *testing.T) {
	for _, tc := range []struct {
		query, candidate string
		match            bool
	}{
		{"sopranos", "The Sopranos", true},
		{"thsp", "The Sopranos", true},
		{"the-sopranos", "The Sopranos", true},
		{"the — sopranos", "The Sopranos", true},
		{"nurse jakie", "Nurse Jackie", true},
		{"nrsjck", "Nurse Jackie", true},
		{"jcakie", "Jackie", false},
		{"carmella", "Carmela Soprano", false},
		{"sopranoss", "The Sopranos", false},
		{"acb", "abc", false},
		{" - ", "The Sopranos", false},
		{"ed", "Edie Falco", true},
		{"ed", "Eddie", true},
		{"edd", "Eddie", true},
		{"edie", "Edie Falco", true},
		{"edie", "Eddie", false},
		{"edie", "Ed-die", false},
		{"ted", "Teed", false},
		{"ed", "Ted", true},
		{"xy", "The Sopranos", false},
		{"nurse", "The Sopranos", false},
	} {
		score := Score(tc.query, tc.candidate)
		if (score >= 0) != tc.match {
			t.Errorf("Score(%q, %q) = %d, match=%v", tc.query, tc.candidate, score, tc.match)
		}
	}
	if Score("Nurse Jakie", "Nurse Jakie") >= Score("Nurse Jakie", "Nurse Jackie") {
		t.Fatal("exact result should outrank a result with skipped characters")
	}
}
