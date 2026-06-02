package scan

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// DefaultQuarantineThreshold is the default number of consecutive wildcard
// responses before a host is quarantined.
const DefaultQuarantineThreshold = 10

type quarantineEntry struct {
	Reason    string
	Timestamp time.Time
}

// Quarantine is a thread-safe registry of hosts that exhibit wildcard routing
// and should be skipped for the remainder of the scan.
type Quarantine struct {
	mu        sync.RWMutex
	entries   map[string]quarantineEntry
	hits      map[string]int
	Threshold int
}

// NewQuarantine returns an initialized Quarantine with the given wildcard hit threshold.
// A threshold ≤ 0 uses DefaultQuarantineThreshold.
func NewQuarantine(threshold int) *Quarantine {
	if threshold <= 0 {
		threshold = DefaultQuarantineThreshold
	}
	return &Quarantine{
		entries:   make(map[string]quarantineEntry),
		hits:      make(map[string]int),
		Threshold: threshold,
	}
}

// Check reports whether host is currently quarantined.
func (q *Quarantine) Check(host string) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	_, ok := q.entries[host]
	return ok
}

// Add unconditionally quarantines host with the given reason.
// Calling Add on an already-quarantined host is a no-op.
func (q *Quarantine) Add(host string, reason string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.entries[host]; exists {
		return
	}
	q.entries[host] = quarantineEntry{
		Reason:    reason,
		Timestamp: time.Now(),
	}
	log.Printf("[INFO] quarantined host %s: %s", host, reason)
}

// QuarantinedHosts returns a snapshot of all currently quarantined host names.
func (q *Quarantine) QuarantinedHosts() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	hosts := make([]string, 0, len(q.entries))
	for h := range q.entries {
		hosts = append(hosts, h)
	}
	return hosts
}

// RecordWildcard records a consecutive wildcard hit for host. When the hit
// count reaches the threshold the host is automatically quarantined.
// Returns true if the host was quarantined on this call.
func (q *Quarantine) RecordWildcard(host string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, ok := q.entries[host]; ok {
		return false
	}

	q.hits[host]++
	if q.hits[host] >= q.Threshold {
		reason := fmt.Sprintf("wildcard: %d consecutive wildcard responses", q.hits[host])
		q.entries[host] = quarantineEntry{
			Reason:    reason,
			Timestamp: time.Now(),
		}
		log.Printf("[INFO] quarantined host %s: %s", host, reason)
		return true
	}
	return false
}
