package postgres

import "testing"

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
