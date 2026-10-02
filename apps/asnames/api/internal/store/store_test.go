package store

import "testing"

func TestLikePattern(t *testing.T) {
	for in, want := range map[string]string{
		"google":        "%google%",
		"100%":          `%100\%%`,
		"under_score":   `%under\_score%`,
		`back\slash`:    `%back\\slash%`,
		`%_\`:           `%\%\_\\%`,
		"côte d'ivoire": "%côte d'ivoire%",
	} {
		if got := LikePattern(in); got != want {
			t.Errorf("%q → %q, quero %q", in, got, want)
		}
	}
}
