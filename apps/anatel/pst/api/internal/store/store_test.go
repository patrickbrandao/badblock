package store

import "testing"

func TestLikePattern(t *testing.T) {
	for in, want := range map[string]string{
		"abc": "%abc%", "100%": `%100\%%`, "a_b": `%a\_b%`, `a\b`: `%a\\b%`, `%_\`: `%\%\_\\%`,
	} {
		if got := LikePattern(in); got != want {
			t.Errorf("LikePattern(%q) = %q, quero %q", in, got, want)
		}
	}
}
