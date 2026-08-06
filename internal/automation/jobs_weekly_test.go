package automation

import "testing"

func TestUniverseRefreshCompletionErrorRejectsEmptyProviderResult(t *testing.T) {
	t.Parallel()

	if err := universeRefreshCompletionError(1); err != nil {
		t.Fatalf("universeRefreshCompletionError(1) = %v, want nil", err)
	}
	if err := universeRefreshCompletionError(0); err == nil {
		t.Fatal("universeRefreshCompletionError(0) = nil, want error")
	}
}
