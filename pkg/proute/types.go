package proute

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// Route represents a single API endpoint with full request context.
type Route struct {
	Method      string
	Path        string
	Headers     []Crumb
	QueryParams []Crumb
	BodyParams  []Crumb
	ContentType string
	Source      string // which wordlist this came from
	KSUID       string // unique identifier for replay
}

// CrumbType enumerates supported parameter value types.
type CrumbType int

const (
	CrumbUUID         CrumbType = iota
	CrumbString       CrumbType = iota
	CrumbInt          CrumbType = iota
	CrumbFloat        CrumbType = iota
	CrumbBool         CrumbType = iota
	CrumbEmail        CrumbType = iota
	CrumbRandomString CrumbType = iota
)

// Crumb represents a single parameter with type information.
type Crumb struct {
	Key      string
	Type     CrumbType
	Required bool
	Example  string
}

const letters = "abcdefghijklmnopqrstuvwxyz"

// GenerateValue returns a realistic synthetic value for the crumb's type.
func (c Crumb) GenerateValue() string {
	if c.Example != "" {
		return c.Example
	}
	switch c.Type {
	case CrumbUUID:
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			rand.Uint32(),
			rand.Uint32()&0xffff,
			(rand.Uint32()&0x0fff)|0x4000,
			(rand.Uint32()&0x3fff)|0x8000,
			rand.Uint64()&0xffffffffffff,
		)
	case CrumbString:
		return "string"
	case CrumbInt:
		return strconv.Itoa(rand.Intn(10000))
	case CrumbFloat:
		return fmt.Sprintf("%.4f", rand.Float64()*1000)
	case CrumbBool:
		if rand.Intn(2) == 0 {
			return "true"
		}
		return "false"
	case CrumbEmail:
		b := make([]byte, 6)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		return string(b) + "@example.com"
	case CrumbRandomString:
		b := make([]byte, 8)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		return string(b)
	default:
		return ""
	}
}

// ScanTarget represents a parsed and validated host/URI.
type ScanTarget struct {
	Scheme   string
	Host     string
	Port     int
	BasePath string
	Raw      string
}

// ScanResult represents a single interesting response.
type ScanResult struct {
	Target        ScanTarget
	Route         Route
	StatusCode    int
	ContentLength int
	ResponseTime  time.Duration
	Timestamp     time.Time
	URL           string
	KSUID         string
}

// LengthRange represents a single value or inclusive range of content lengths.
type LengthRange struct {
	Min int
	Max int
}

// ParseLengthRange parses "1234" or "100-105" into a LengthRange.
func ParseLengthRange(s string) (LengthRange, error) {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "-"); idx > 0 {
		minStr := s[:idx]
		maxStr := s[idx+1:]
		min, err := strconv.Atoi(minStr)
		if err != nil {
			return LengthRange{}, fmt.Errorf("invalid range min %q: %w", minStr, err)
		}
		max, err := strconv.Atoi(maxStr)
		if err != nil {
			return LengthRange{}, fmt.Errorf("invalid range max %q: %w", maxStr, err)
		}
		return LengthRange{Min: min, Max: max}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return LengthRange{}, fmt.Errorf("invalid length %q: %w", s, err)
	}
	return LengthRange{Min: n, Max: n}, nil
}

// Contains reports whether n falls within the length range (inclusive).
func (lr LengthRange) Contains(n int) bool {
	return n >= lr.Min && n <= lr.Max
}

// ScanConfig holds all runtime scan parameters.
type ScanConfig struct {
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
	IgnoreLengths        []LengthRange
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
