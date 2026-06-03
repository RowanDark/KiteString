package input

import (
	"fmt"
	"io"
	"os"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// ReadTargets resolves scan targets from arg and stdin according to these rules:
//   - arg == "-" or arg == "": read from stdin using ParseInputStream
//   - arg is an existing file path: read from that file using ParseInputStream
//   - otherwise: parse arg directly as a host/URI via ParseTarget
//
// Duplicate targets (same scheme + host + port) are silently removed.
func ReadTargets(arg string, stdin io.Reader) ([]proute.ScanTarget, error) {
	var (
		targets []proute.ScanTarget
		err     error
	)

	switch {
	case arg == "" || arg == "-":
		targets, err = proute.ParseInputStream(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}

	default:
		if _, statErr := os.Stat(arg); statErr == nil {
			f, openErr := os.Open(arg)
			if openErr != nil {
				return nil, fmt.Errorf("opening %s: %w", arg, openErr)
			}
			defer f.Close()
			targets, err = proute.ParseInputStream(f)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", arg, err)
			}
		} else {
			targets, err = proute.ParseTarget(arg)
			if err != nil {
				return nil, err
			}
		}
	}

	return deduplicate(targets), nil
}

func deduplicate(targets []proute.ScanTarget) []proute.ScanTarget {
	seen := make(map[string]bool, len(targets))
	result := make([]proute.ScanTarget, 0, len(targets))
	for _, t := range targets {
		key := fmt.Sprintf("%s://%s:%d", t.Scheme, t.Host, t.Port)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	return result
}
