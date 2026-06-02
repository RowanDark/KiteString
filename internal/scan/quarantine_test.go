package scan

import (
	"sync"
	"testing"
)

func TestQuarantine_InitiallyNotQuarantined(t *testing.T) {
	q := NewQuarantine(10)
	if q.Check("example.com") {
		t.Error("host should not be quarantined initially")
	}
}

func TestQuarantine_Add(t *testing.T) {
	q := NewQuarantine(10)
	q.Add("example.com", "manual quarantine")
	if !q.Check("example.com") {
		t.Error("host should be quarantined after Add")
	}
}

func TestQuarantine_Add_Idempotent(t *testing.T) {
	q := NewQuarantine(10)
	q.Add("example.com", "first reason")
	q.Add("example.com", "second reason") // should not panic or change state
	if !q.Check("example.com") {
		t.Error("host should still be quarantined")
	}
}

func TestQuarantine_RecordWildcard_FiresAtThreshold(t *testing.T) {
	const threshold = 3
	q := NewQuarantine(threshold)
	host := "wildcard.example.com"

	for i := 0; i < threshold-1; i++ {
		fired := q.RecordWildcard(host)
		if fired {
			t.Errorf("quarantine fired early at hit %d (threshold %d)", i+1, threshold)
		}
		if q.Check(host) {
			t.Errorf("host quarantined early at hit %d (threshold %d)", i+1, threshold)
		}
	}

	// Final hit should trigger quarantine.
	fired := q.RecordWildcard(host)
	if !fired {
		t.Error("expected RecordWildcard to return true when threshold is reached")
	}
	if !q.Check(host) {
		t.Error("host should be quarantined after reaching threshold")
	}
}

func TestQuarantine_RecordWildcard_NotFiredBefore(t *testing.T) {
	q := NewQuarantine(5)
	host := "almost.example.com"

	for i := 0; i < 4; i++ {
		q.RecordWildcard(host)
	}
	if q.Check(host) {
		t.Error("host should not be quarantined before threshold is reached")
	}
}

func TestQuarantine_RecordWildcard_NoopWhenAlreadyQuarantined(t *testing.T) {
	q := NewQuarantine(2)
	host := "example.com"

	q.RecordWildcard(host)
	q.RecordWildcard(host) // quarantines

	// Additional calls should not panic and should return false.
	fired := q.RecordWildcard(host)
	if fired {
		t.Error("RecordWildcard should return false when host is already quarantined")
	}
}

func TestQuarantine_DefaultThreshold(t *testing.T) {
	q := NewQuarantine(0) // 0 → DefaultQuarantineThreshold
	if q.Threshold != DefaultQuarantineThreshold {
		t.Errorf("threshold = %d, want %d", q.Threshold, DefaultQuarantineThreshold)
	}
}

func TestQuarantine_ConcurrentAccess(t *testing.T) {
	q := NewQuarantine(5)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.RecordWildcard("concurrent.example.com")
			q.Check("concurrent.example.com")
		}()
	}
	wg.Wait()

	// Host must be quarantined after enough concurrent hits.
	if !q.Check("concurrent.example.com") {
		t.Error("host should be quarantined after concurrent wildcard hits exceed threshold")
	}
}
