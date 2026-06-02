package wordlist

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RowanDark/kitestring/pkg/ksfile"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// Load detects the format of each path by extension, loads all routes,
// deduplicates by method+path, and returns the merged slice.
func Load(paths []string) ([]proute.Route, error) {
	seen := make(map[string]struct{})
	var merged []proute.Route

	for _, p := range paths {
		routes, err := loadOne(p)
		if err != nil {
			return nil, err
		}
		for _, r := range routes {
			key := strings.ToUpper(r.Method) + "\x00" + r.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, r)
		}
	}
	return merged, nil
}

// Head returns the first n routes from each wordlist file, then merges and
// deduplicates. If n <= 0 all routes are returned (equivalent to no limit).
func Head(paths []string, n int) ([]proute.Route, error) {
	if n <= 0 {
		return Load(paths)
	}

	seen := make(map[string]struct{})
	var merged []proute.Route

	for _, p := range paths {
		routes, err := loadOne(p)
		if err != nil {
			return nil, err
		}
		count := 0
		for _, r := range routes {
			if count >= n {
				break
			}
			key := strings.ToUpper(r.Method) + "\x00" + r.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, r)
			count++
		}
	}
	return merged, nil
}

func loadOne(path string) ([]proute.Route, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("wordlist: file not found: %s", path)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".ks":
		return LoadKS(path)
	case ".txt":
		return LoadTXT(path)
	case ".json":
		return LoadJSON(path)
	default:
		return nil, fmt.Errorf("wordlist: unsupported extension %q (use .ks, .txt, or .json)", filepath.Ext(path))
	}
}

// LoadKS loads a .ks binary wordlist file.
func LoadKS(path string) ([]proute.Route, error) {
	kf, err := ksfile.Read(path)
	if err != nil {
		return nil, fmt.Errorf("wordlist: corrupt or unreadable .ks file %q: %w", path, err)
	}
	routes, err := ksfile.ToRoutes(kf)
	if err != nil {
		return nil, fmt.Errorf("wordlist: convert .ks routes %q: %w", path, err)
	}
	return routes, nil
}

// LoadTXT loads a flat text file where each non-blank, non-comment line is
// treated as a URL path for a GET request.
func LoadTXT(path string) ([]proute.Route, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("wordlist: open %q: %w", path, err)
	}
	defer f.Close()

	var routes []proute.Route
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		routes = append(routes, proute.Route{
			Method: "GET",
			Path:   line,
			Source: path,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("wordlist: read %q: %w", path, err)
	}
	return routes, nil
}

// jsonRoute is the expected schema for entries in a .json wordlist file.
type jsonRoute struct {
	Method      string      `json:"method"`
	Path        string      `json:"path"`
	Headers     []jsonCrumb `json:"headers"`
	QueryParams []jsonCrumb `json:"query_params"`
	BodyParams  []jsonCrumb `json:"body_params"`
}

type jsonCrumb struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Example  string `json:"example"`
}

// LoadJSON loads a structured JSON wordlist file containing an array of route
// objects.
func LoadJSON(path string) ([]proute.Route, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wordlist: open %q: %w", path, err)
	}

	var raw []jsonRoute
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("wordlist: malformed JSON in %q: %w", path, err)
	}

	routes := make([]proute.Route, 0, len(raw))
	for _, jr := range raw {
		method := strings.ToUpper(strings.TrimSpace(jr.Method))
		if method == "" {
			method = "GET"
		}
		r := proute.Route{
			Method:      method,
			Path:        jr.Path,
			Source:      path,
			Headers:     convertCrumbs(jr.Headers),
			QueryParams: convertCrumbs(jr.QueryParams),
			BodyParams:  convertCrumbs(jr.BodyParams),
		}
		routes = append(routes, r)
	}
	return routes, nil
}

func convertCrumbs(jcs []jsonCrumb) []proute.Crumb {
	if len(jcs) == 0 {
		return nil
	}
	out := make([]proute.Crumb, len(jcs))
	for i, jc := range jcs {
		out[i] = proute.Crumb{
			Key:      jc.Key,
			Type:     parseCrumbType(jc.Type),
			Required: jc.Required,
			Example:  jc.Example,
		}
	}
	return out
}

func parseCrumbType(s string) proute.CrumbType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "uuid":
		return proute.CrumbUUID
	case "int", "integer":
		return proute.CrumbInt
	case "float", "number":
		return proute.CrumbFloat
	case "bool", "boolean":
		return proute.CrumbBool
	case "email":
		return proute.CrumbEmail
	case "random":
		return proute.CrumbRandomString
	default:
		return proute.CrumbString
	}
}
