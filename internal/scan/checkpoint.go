package scan

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

const checkpointVersion = 1

// Checkpoint persists scan state so an interrupted scan can be resumed.
type Checkpoint struct {
	Version       int
	ScanID        string
	StartedAt     time.Time
	UpdatedAt     time.Time
	Config        proute.ScanConfig
	Targets       []proute.ScanTarget
	WordlistPaths []string
	CompletedKeys []string // "METHOD:host:path" composite keys already scanned
	Results       []proute.ScanResult
	Quarantined   []string

	mu   sync.Mutex
	keys map[string]struct{} // in-memory set for O(1) IsComplete lookups
}

// scanConfigJSON is a JSON-serializable mirror of proute.ScanConfig that omits
// the ScopeCheck function field, which cannot be encoded.
type scanConfigJSON struct {
	MaxConnPerHost       int                  `json:"max_conn_per_host"`
	MaxParallelHosts     int                  `json:"max_parallel_hosts"`
	Timeout              time.Duration        `json:"timeout"`
	Delay                time.Duration        `json:"delay"`
	MaxRetries           int                  `json:"max_retries"`
	BackoffBase          time.Duration        `json:"backoff_base"`
	BackoffMax           time.Duration        `json:"backoff_max"`
	UnreachableThreshold int                  `json:"unreachable_threshold"`
	FailStatusCodes      []int                `json:"fail_status_codes"`
	SuccessStatusCodes   []int                `json:"success_status_codes"`
	IgnoreLengths        []proute.LengthRange `json:"ignore_lengths"`
	Headers              []string             `json:"headers"`
	UserAgent            string               `json:"user_agent"`
	MaxRedirects         int                  `json:"max_redirects"`
	WildcardDetection    bool                 `json:"wildcard_detection"`
	QuarantineThresh     int                  `json:"quarantine_thresh"`
	OutputFormat         string               `json:"output_format"`
	DisablePreflight     bool                 `json:"disable_preflight"`
	PreflightDepth       int                  `json:"preflight_depth"`
	FilterAPIKSUID       string               `json:"filter_api_ksuid"`
	ForceMethod          string               `json:"force_method"`
	BlacklistDomains     []string             `json:"blacklist_domains"`
	FullScan             bool                 `json:"full_scan"`
	SimilarityThreshold  float64              `json:"similarity_threshold"`
	DisableSimilarity    bool                 `json:"disable_similarity"`
	Verbose              string               `json:"verbose"`
}

// checkpointJSON is the on-disk representation of a Checkpoint.
type checkpointJSON struct {
	Version       int                 `json:"version"`
	ScanID        string              `json:"scan_id"`
	StartedAt     time.Time           `json:"started_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Config        scanConfigJSON      `json:"config"`
	Targets       []proute.ScanTarget `json:"targets"`
	WordlistPaths []string            `json:"wordlist_paths"`
	CompletedKeys []string            `json:"completed_keys"`
	Results       []proute.ScanResult `json:"results"`
	Quarantined   []string            `json:"quarantined"`
}

// NewCheckpoint creates a new checkpoint with a fresh ScanID and initial state.
func NewCheckpoint(config proute.ScanConfig, targets []proute.ScanTarget, wordlists []string) *Checkpoint {
	return &Checkpoint{
		Version:       checkpointVersion,
		ScanID:        newUUID(),
		StartedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Config:        config,
		Targets:       targets,
		WordlistPaths: wordlists,
		keys:          make(map[string]struct{}),
	}
}

// Save serializes the checkpoint to path atomically by writing to a temp file
// then renaming, so a partial write never corrupts the previous checkpoint.
func (c *Checkpoint) Save(path string) error {
	c.mu.Lock()
	c.UpdatedAt = time.Now()
	j := checkpointJSON{
		Version:       c.Version,
		ScanID:        c.ScanID,
		StartedAt:     c.StartedAt,
		UpdatedAt:     c.UpdatedAt,
		Config:        configToJSON(c.Config),
		Targets:       c.Targets,
		WordlistPaths: c.WordlistPaths,
		CompletedKeys: c.CompletedKeys,
		Results:       c.Results,
		Quarantined:   c.Quarantined,
	}
	c.mu.Unlock()

	data, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("checkpoint marshal: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("checkpoint mkdir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("checkpoint write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return fmt.Errorf("checkpoint rename: %w", err)
	}
	return nil
}

// Load deserializes a checkpoint from path and validates the version.
// It must be called on a zero-value Checkpoint.
func (c *Checkpoint) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("checkpoint read: %w", err)
	}

	var j checkpointJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("checkpoint unmarshal: %w", err)
	}
	if j.Version != checkpointVersion {
		return fmt.Errorf("checkpoint version mismatch: got %d, want %d", j.Version, checkpointVersion)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.Version = j.Version
	c.ScanID = j.ScanID
	c.StartedAt = j.StartedAt
	c.UpdatedAt = j.UpdatedAt
	c.Config = configFromJSON(j.Config)
	c.Targets = j.Targets
	c.WordlistPaths = j.WordlistPaths
	c.CompletedKeys = j.CompletedKeys
	c.Results = j.Results
	c.Quarantined = j.Quarantined

	c.keys = make(map[string]struct{}, len(c.CompletedKeys))
	for _, k := range c.CompletedKeys {
		c.keys[k] = struct{}{}
	}
	return nil
}

// MarkComplete records that the given method+host+path combination has been scanned.
// It is safe to call concurrently.
func (c *Checkpoint) MarkComplete(method, host, path string) {
	key := method + ":" + host + ":" + path
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.keys[key]; !exists {
		c.keys[key] = struct{}{}
		c.CompletedKeys = append(c.CompletedKeys, key)
	}
}

// IsComplete reports whether the given method+host+path combination has already
// been scanned. It is safe to call concurrently.
func (c *Checkpoint) IsComplete(method, host, path string) bool {
	key := method + ":" + host + ":" + path
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.keys[key]
	return ok
}

// RemainingRoutes returns the subset of all that have not yet been scanned for
// the given target.
func (c *Checkpoint) RemainingRoutes(all []proute.Route, target proute.ScanTarget) []proute.Route {
	remaining := make([]proute.Route, 0, len(all))
	for _, r := range all {
		if !c.IsComplete(r.Method, target.Host, r.Path) {
			remaining = append(remaining, r)
		}
	}
	return remaining
}

// AddResult appends a scan result to the checkpoint's result list.
// It is safe to call concurrently.
func (c *Checkpoint) AddResult(r proute.ScanResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Results = append(c.Results, r)
}

func configToJSON(c proute.ScanConfig) scanConfigJSON {
	return scanConfigJSON{
		MaxConnPerHost:       c.MaxConnPerHost,
		MaxParallelHosts:     c.MaxParallelHosts,
		Timeout:              c.Timeout,
		Delay:                c.Delay,
		MaxRetries:           c.MaxRetries,
		BackoffBase:          c.BackoffBase,
		BackoffMax:           c.BackoffMax,
		UnreachableThreshold: c.UnreachableThreshold,
		FailStatusCodes:      c.FailStatusCodes,
		SuccessStatusCodes:   c.SuccessStatusCodes,
		IgnoreLengths:        c.IgnoreLengths,
		Headers:              c.Headers,
		UserAgent:            c.UserAgent,
		MaxRedirects:         c.MaxRedirects,
		WildcardDetection:    c.WildcardDetection,
		QuarantineThresh:     c.QuarantineThresh,
		OutputFormat:         c.OutputFormat,
		DisablePreflight:     c.DisablePreflight,
		PreflightDepth:       c.PreflightDepth,
		FilterAPIKSUID:       c.FilterAPIKSUID,
		ForceMethod:          c.ForceMethod,
		BlacklistDomains:     c.BlacklistDomains,
		FullScan:             c.FullScan,
		SimilarityThreshold:  c.SimilarityThreshold,
		DisableSimilarity:    c.DisableSimilarity,
		Verbose:              c.Verbose,
	}
}

func configFromJSON(j scanConfigJSON) proute.ScanConfig {
	return proute.ScanConfig{
		MaxConnPerHost:       j.MaxConnPerHost,
		MaxParallelHosts:     j.MaxParallelHosts,
		Timeout:              j.Timeout,
		Delay:                j.Delay,
		MaxRetries:           j.MaxRetries,
		BackoffBase:          j.BackoffBase,
		BackoffMax:           j.BackoffMax,
		UnreachableThreshold: j.UnreachableThreshold,
		FailStatusCodes:      j.FailStatusCodes,
		SuccessStatusCodes:   j.SuccessStatusCodes,
		IgnoreLengths:        j.IgnoreLengths,
		Headers:              j.Headers,
		UserAgent:            j.UserAgent,
		MaxRedirects:         j.MaxRedirects,
		WildcardDetection:    j.WildcardDetection,
		QuarantineThresh:     j.QuarantineThresh,
		OutputFormat:         j.OutputFormat,
		DisablePreflight:     j.DisablePreflight,
		PreflightDepth:       j.PreflightDepth,
		FilterAPIKSUID:       j.FilterAPIKSUID,
		ForceMethod:          j.ForceMethod,
		BlacklistDomains:     j.BlacklistDomains,
		FullScan:             j.FullScan,
		SimilarityThreshold:  j.SimilarityThreshold,
		DisableSimilarity:    j.DisableSimilarity,
		Verbose:              j.Verbose,
		// ScopeCheck is always nil after load; the CLI re-wires it at runtime.
	}
}

// newUUID generates a random UUID v4 string without external dependencies.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ks-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
