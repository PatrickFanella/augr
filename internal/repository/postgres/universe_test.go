package postgres

import (
	"strings"
	"testing"
)

func TestCanonicalUniverseTicker(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"already canonical": "AAPL",
		"case variant":      "FDXW",
		"class symbol":      "BF.B",
		"empty":             "",
	}
	inputs := map[string]string{
		"already canonical": "AAPL",
		"case variant":      "  FDXw ",
		"class symbol":      " bf.b ",
		"empty":             " \t ",
	}

	for name, want := range tests {
		name, input, want := name, inputs[name], want
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalUniverseTicker(input); got != want {
				t.Fatalf("canonicalUniverseTicker(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestUniverseWatchlistQueryCanonicalizesAndDeduplicates(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"DISTINCT ON (upper(trim(ticker)))",
		"upper(trim(ticker)) AS normalized_ticker",
		"(ticker = upper(trim(ticker))) DESC",
	} {
		if !strings.Contains(universeWatchlistQuery, want) {
			t.Fatalf("universeWatchlistQuery missing %q", want)
		}
	}
}
