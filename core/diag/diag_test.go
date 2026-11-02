package diag

import (
	"testing"
)

func TestDiagnostics(t *testing.T) {
	results := RunDiagnostics()
	if len(results) == 0 {
		t.Fatalf("expected diagnostic results, got 0")
	}

	for _, res := range results {
		t.Logf("Diagnostic [%s]: Passed=%v, Message=%s", res.Name, res.Passed, res.Message)
	}
}
