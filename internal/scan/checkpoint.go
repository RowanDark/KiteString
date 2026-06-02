package scan

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

const checkpointVersion = 1

// checkpointConfig is a JSON-serializable mirror of proute.ScanConfig.
// The Scope interface field is excluded; it is re-applied from CLI flags on resume.
type checkpointConfig struct {
	MaxConnPerHost       int
	MaxParallelHosts     int
	Timeout              time.Duration
	Delay                time.Duration
	MaxRetries           int
	BackoffBase          time.Duration
	BackoffMax           time.Duration
	UnreachableThreshold int
	FailStatusCodes      []int
	SuccessStatusCodes   []int
	IgnoreLengths        []proute.LengthRange
	Headers              []string
	UserAgent            string
	MaxRedirects         int
	WildcardDetection    bool
	QuarantineThresh     int
	OutputFormat         string
	DisablePreflight     bool
	PreflightDepth       int
	FilterAPIKSUID       string
	ForceMethod          string
	BlacklistDomains     []string
	FullScan             bool
	SimilarityThreshold  float64
	DisableSimilarity    bool
	Verbose              string
}

// Checkpoint persists scan state so that an interrupted scan can be resumed
// from where it left off rather than restarted from scratch.
type Checkpoint struct {
	Version       int                 `json:"Version"`
	ScanID        string              `json:"ScanID"`
	StartedAt     time.Time           `json:"StartedAt"`
	UpdatedAt     time.Time           `json:"UpdatedAt"`
	Config        checkpointConfig    `json:"Config"`
	Targets       []proute.ScanTarget `json:"Targets"`
	WordlistPaths []string            `json:"WordlistPaths"`
	CompletedKeys []string            `json:"CompletedKeys"`
	Results       []proute.ScanResult `json:"Results"`
	Quarantined   []string            `json:"Quarantined"`

	// runtime state — not serialized (unexported fields are skipped by encoding/json)
	mu           sync.Mutex
	completedSet map[string]struct{}
}

// NewCheckpoint creates a fresh Checkpoint with a generated ScanID.
func NewCheckpoint(config proute.ScanConfig, targets []proute.ScanTarget, wordlists []string) *Checkpoint {
	return &Checkpoint{
		Version:       checkpointVersion,
		ScanID:        newScanID(),
		StartedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Config:        configToCheckpoint(config),
		Targets:       targets,
		WordlistPaths: wordlists,
		completedSet:  make(map[string]struct{}),
	}
}

// Save atomically writes the checkpoint to path by writing a temp file then renaming it,
// so a partial write never corrupts the previous checkpoint.
func (c *Checkpoint) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("checkpoint dir: %w", err)
	}

	c.mu.Lock()
	c.UpdatedAt = time.Now()
	c.CompletedKeys = setToSlice(c.completedSet)
	data, err := json.MarshalIndent(c, "", "  ")
	c.mu.Unlock()

	if err != nil {
		return fmt.Errorf("checkpoint marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("checkpoint write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("checkpoint rename: %w", err)
	}
	return nil
}

// Load reads and deserializes a checkpoint from path, rebuilding the
// in-memory completedSet from the persisted CompletedKeys slice.
func (c *Checkpoint) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("checkpoint read: %w", err)
	}
	if err := json.Unmarshal(data, c); err != nil {
		return fmt.Errorf("checkpoint unmarshal: %w", err)
	}
	if c.Version != checkpointVersion {
		return fmt.Errorf("checkpoint version mismatch: got %d, want %d", c.Version, checkpointVersion)
	}
	c.mu.Lock()
	c.completedSet = sliceToSet(c.CompletedKeys)
	c.mu.Unlock()
	return nil
}

// MarkComplete records the METHOD:host:path triplet as processed. Thread-safe.
func (c *Checkpoint) MarkComplete(method, host, path string) {
	key := method + ":" + host + ":" + path
	c.mu.Lock()
	c.completedSet[key] = struct{}{}
	c.mu.Unlock()
}

// IsComplete reports whether the METHOD:host:path triplet has already been processed. Thread-safe.
func (c *Checkpoint) IsComplete(method, host, path string) bool {
	key := method + ":" + host + ":" + path
	c.mu.Lock()
	_, ok := c.completedSet[key]
	c.mu.Unlock()
	return ok
}

// RemainingRoutes returns the subset of all that have not yet been completed for target.
func (c *Checkpoint) RemainingRoutes(all []proute.Route, target proute.ScanTarget) []proute.Route {
	remaining := make([]proute.Route, 0, len(all))
	for _, r := range all {
		if !c.IsComplete(r.Method, target.Host, r.Path) {
			remaining = append(remaining, r)
		}
	}
	return remaining
}

// AddResult appends a passing scan result to the checkpoint. Thread-safe.
func (c *Checkpoint) AddResult(result proute.ScanResult) {
	c.mu.Lock()
	c.Results = append(c.Results, result)
	c.mu.Unlock()
}

// SetQuarantined replaces the persisted quarantined host list. Thread-safe.
func (c *Checkpoint) SetQuarantined(hosts []string) {
	c.mu.Lock()
	c.Quarantined = hosts
	c.mu.Unlock()
}

// CompletedCount returns the number of completed METHOD:host:path pairs. Thread-safe.
func (c *Checkpoint) CompletedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.completedSet)
}

// newScanID generates a random UUID v4 string.
func newScanID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

func configToCheckpoint(c proute.ScanConfig) checkpointConfig {
	return checkpointConfig{
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

func setToSlice(m map[string]struct{}) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	return s
}

func sliceToSet(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, k := range s {
		m[k] = struct{}{}
	}
	return m
}
