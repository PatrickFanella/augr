package postgres

import (
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/universe"
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

func TestBuildUniverseListQueryCanonicalizesBeforePagination(t *testing.T) {
	t.Parallel()

	active := true
	query, args := buildUniverseListQuery(universe.ListFilter{
		IndexGroup: "nasdaq",
		Active:     &active,
		Search:     "fdxw",
	}, 25, 50)
	for _, want := range []string{
		"DISTINCT ON (upper(trim(ticker)))",
		"upper(trim(ticker)) AS normalized_ticker",
		"(ticker = upper(trim(ticker))) DESC",
		"ORDER BY watch_score DESC, normalized_ticker",
		"LIMIT $5 OFFSET $6",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("buildUniverseListQuery() missing %q in %q", want, query)
		}
	}
	if len(args) != 6 || args[0] != "nasdaq" || args[4] != 25 || args[5] != 50 {
		t.Fatalf("buildUniverseListQuery() args = %#v, want filters then 25/50 pagination", args)
	}
}

func TestUniverseCountQueryUsesCanonicalIdentity(t *testing.T) {
	t.Parallel()

	if !strings.Contains(universeCountQuery, "COUNT(DISTINCT upper(trim(ticker)))") {
		t.Fatalf("universeCountQuery = %q, want canonical distinct count", universeCountQuery)
	}
}
