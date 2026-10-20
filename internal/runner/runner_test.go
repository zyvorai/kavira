package runner

import (
	"testing"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
)

func TestRunConfigRejectsNonPositiveBatch(t *testing.T) {
	for _, batch := range []string{"0", "-1", "99999999"} {
		res := RunConfig(map[string]string{"batch_size": batch})
		if res.Passed || res.Failures != 1 {
			t.Fatalf("batch=%s: expected failure, got %+v", batch, res)
		}
	}
}

func TestRunNetworkDialFailureDoesNotHang(t *testing.T) {
	done := make(chan Result, 1)
	go func() {
		// Policy-deny closes every connection; the run must still return.
		done <- RunNetwork(bundle.NetworkCondition{Attempts: 3, TimeoutMs: 50, PolicyDeny: true})
	}()
	select {
	case res := <-done:
		if res.Passed {
			t.Fatalf("expected failures, got %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunNetwork hung")
	}
}
