package input

import (
	"os"
	"strings"
	"testing"
)

func TestReadTargetsDirectArg(t *testing.T) {
	targets, err := ReadTargets("https://example.com", strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Host != "example.com" || targets[0].Scheme != "https" {
		t.Errorf("unexpected target: %+v", targets[0])
	}
}

func TestReadTargetsBareDomainDirectArg(t *testing.T) {
	// Bare domain expands to both http and https via ParseTarget.
	targets, err := ReadTargets("example.com", strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets (http+https), got %d", len(targets))
	}
}

func TestReadTargetsStdinDash(t *testing.T) {
	stdin := strings.NewReader("https://alpha.com\nhttps://beta.com\n")
	targets, err := ReadTargets("-", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
}

func TestReadTargetsEmptyArgFromStdin(t *testing.T) {
	stdin := strings.NewReader("https://example.com\n")
	targets, err := ReadTargets("", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
}

func TestReadTargetsDeduplication(t *testing.T) {
	stdin := strings.NewReader(
		"https://example.com\n" +
			"https://example.com\n" +
			"https://other.com\n",
	)
	targets, err := ReadTargets("-", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Errorf("expected 2 targets after dedup, got %d", len(targets))
	}
}

func TestReadTargetsMixedFormatDeduplication(t *testing.T) {
	// httpx JSON and plain URL pointing to the same host should deduplicate.
	stdin := strings.NewReader(
		`{"url":"https://example.com","status_code":200,"tech":["nginx"]}` + "\n" +
			"https://example.com\n",
	)
	targets, err := ReadTargets("-", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("expected 1 target after dedup, got %d", len(targets))
	}
}

func TestReadTargetsFileArg(t *testing.T) {
	f, err := os.CreateTemp("", "ks-input-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	_, _ = f.WriteString("https://alpha.com\nhttps://beta.com\n# comment\n\n")
	f.Close()

	targets, err := ReadTargets(f.Name(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(targets))
	}
}

func TestReadTargetsHTTPXPipeline(t *testing.T) {
	// Simulate httpx standard output piped to stdin.
	stdin := strings.NewReader(
		"https://alpha.com [200] [Alpha] [nginx,php]\n" +
			"https://beta.com [200] [Beta] [apache]\n" +
			"# this line is a comment\n" +
			"\n" +
			"https://alpha.com [301] [Alpha Redirect] [nginx,php]\n", // duplicate
	)
	targets, err := ReadTargets("-", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Errorf("expected 2 unique targets, got %d", len(targets))
	}
	// Tags preserved on first occurrence.
	if len(targets[0].Tags) == 0 {
		t.Errorf("expected tech tags on first target, got none")
	}
}
