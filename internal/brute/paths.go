package brute

import "strings"

// ExpandExtensions appends each extension to each path, returning one variant per extension per path.
// Extensions without a leading dot have one added automatically.
func ExpandExtensions(paths, extensions []string) []string {
	if len(extensions) == 0 {
		return paths
	}
	result := make([]string, 0, len(paths)*len(extensions))
	for _, p := range paths {
		for _, ext := range extensions {
			if ext != "" && !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			result = append(result, p+ext)
		}
	}
	return result
}

// ExpandDirsearch substitutes the %EXT% placeholder in paths with each extension value.
// Paths without %EXT% are passed through unchanged.
func ExpandDirsearch(paths, extensions []string) []string {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.Contains(p, "%EXT%") {
			for _, ext := range extensions {
				result = append(result, strings.ReplaceAll(p, "%EXT%", ext))
			}
		} else {
			result = append(result, p)
		}
	}
	return result
}

// Deduplicate removes duplicate paths while preserving insertion order.
func Deduplicate(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			result = append(result, p)
		}
	}
	return result
}
