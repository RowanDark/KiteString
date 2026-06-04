package cli

import (
	"testing"
)

// TestCommandAliases verifies that all defined command aliases resolve to the correct command.
func TestCommandAliases(t *testing.T) {
	tests := []struct {
		args        []string
		wantName    string
		description string
	}{
		// Top-level aliases
		{[]string{"s"}, "scan", "ks s → ks scan"},
		{[]string{"b"}, "brute", "ks b → ks brute"},
		{[]string{"fuzz"}, "brute", "ks fuzz → ks brute"},
		{[]string{"r"}, "replay", "ks r → ks replay"},
		{[]string{"rp"}, "report", "ks rp → ks report"},
		{[]string{"wl"}, "wordlist", "ks wl → ks wordlist"},
		{[]string{"w"}, "wordlist", "ks w → ks wordlist"},
		{[]string{"p"}, "profile", "ks p → ks profile"},

		// Wordlist subcommand aliases
		{[]string{"wordlist", "ls"}, "list", "ks wordlist ls → ks wordlist list"},
		{[]string{"wordlist", "l"}, "list", "ks wordlist l → ks wordlist list"},
		{[]string{"wl", "ls"}, "list", "ks wl ls → ks wordlist list"},
		{[]string{"wl", "l"}, "list", "ks wl l → ks wordlist list"},
		{[]string{"wordlist", "up"}, "update", "ks wordlist up → ks wordlist update"},
		{[]string{"wordlist", "u"}, "update", "ks wordlist u → ks wordlist update"},
		{[]string{"wordlist", "c"}, "compile", "ks wordlist c → ks wordlist compile"},
		{[]string{"wordlist", "sl"}, "seclists", "ks wordlist sl → ks wordlist seclists"},
		{[]string{"wordlist", "oa"}, "openapi", "ks wordlist oa → ks wordlist openapi"},

		// Profile subcommand aliases
		{[]string{"profile", "ls"}, "list", "ks profile ls → ks profile list"},
		{[]string{"profile", "l"}, "list", "ks profile l → ks profile list"},
		{[]string{"profile", "s"}, "show", "ks profile s → ks profile show"},
		{[]string{"p", "ls"}, "list", "ks p ls → ks profile list"},
		{[]string{"p", "l"}, "list", "ks p l → ks profile list"},
		{[]string{"p", "s"}, "show", "ks p s → ks profile show"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tt.args)
			if err != nil {
				t.Fatalf("Find(%v) error: %v", tt.args, err)
			}
			if cmd == nil {
				t.Fatalf("Find(%v) returned nil", tt.args)
			}
			if cmd.Name() != tt.wantName {
				t.Errorf("Find(%v) = %q, want %q", tt.args, cmd.Name(), tt.wantName)
			}
		})
	}
}

// TestShortFlags verifies that all specified short flags are registered and resolve
// to their correct long-form flag names.
func TestShortFlags(t *testing.T) {
	type flagTest struct {
		cmdName  string
		short    string
		wantName string
	}

	scanTests := []flagTest{
		{"scan", "w", "wordlist"},
		{"scan", "A", "wordlist-alias"},
		{"scan", "S", "seclists"},
		{"scan", "p", "profile"},
		{"scan", "P", "proxy"},
		{"scan", "s", "scope-file"},
		{"scan", "c", "checkpoint"},
		{"scan", "r", "resume"},
		{"scan", "R", "report"},
		{"scan", "J", "js-extract"},
		{"scan", "O", "openapi-url"},
		{"scan", "t", "threads"},
		{"scan", "j", "parallel-hosts"},
		{"scan", "H", "header"},
		{"scan", "d", "depth"},
	}

	bruteTests := []flagTest{
		{"brute", "w", "wordlist"},
		{"brute", "A", "wordlist-alias"},
		{"brute", "S", "seclists"},
		{"brute", "e", "extensions"},
		{"brute", "p", "profile"},
		{"brute", "P", "proxy"},
		{"brute", "R", "report"},
		{"brute", "t", "threads"},
		{"brute", "j", "parallel-hosts"},
		{"brute", "H", "header"},
		{"brute", "r", "follow-redirects"},
	}

	replayTests := []flagTest{
		{"replay", "w", "wordlist"},
		{"replay", "P", "proxy"},
		{"replay", "Q", "show-request"},
		{"replay", "Z", "show-response"},
	}

	for _, tt := range scanTests {
		t.Run("scan-"+tt.short, func(t *testing.T) {
			f := scanCmd.Flags().ShorthandLookup(tt.short)
			if f == nil {
				t.Fatalf("scan: short flag -%s not found", tt.short)
			}
			if f.Name != tt.wantName {
				t.Errorf("scan: -%s resolves to %q, want %q", tt.short, f.Name, tt.wantName)
			}
		})
	}

	for _, tt := range bruteTests {
		t.Run("brute-"+tt.short, func(t *testing.T) {
			f := bruteCmd.Flags().ShorthandLookup(tt.short)
			if f == nil {
				t.Fatalf("brute: short flag -%s not found", tt.short)
			}
			if f.Name != tt.wantName {
				t.Errorf("brute: -%s resolves to %q, want %q", tt.short, f.Name, tt.wantName)
			}
		})
	}

	for _, tt := range replayTests {
		t.Run("replay-"+tt.short, func(t *testing.T) {
			f := replayCmd.Flags().ShorthandLookup(tt.short)
			if f == nil {
				t.Fatalf("replay: short flag -%s not found", tt.short)
			}
			if f.Name != tt.wantName {
				t.Errorf("replay: -%s resolves to %q, want %q", tt.short, f.Name, tt.wantName)
			}
		})
	}
}

// TestSafetyFlagsLongFormOnly ensures that --disable-precheck and --tls-skip-verify
// have no short-form flags (they are intentionally long-form only for safety).
func TestSafetyFlagsLongFormOnly(t *testing.T) {
	disablePrecheck := scanCmd.Flags().Lookup("disable-precheck")
	if disablePrecheck == nil {
		t.Fatal("scan: --disable-precheck flag not found")
	}
	if disablePrecheck.Shorthand != "" {
		t.Errorf("scan: --disable-precheck should have no shorthand, got -%s", disablePrecheck.Shorthand)
	}

	tlsSkipVerify := replayCmd.Flags().Lookup("tls-skip-verify")
	if tlsSkipVerify == nil {
		t.Fatal("replay: --tls-skip-verify flag not found")
	}
	if tlsSkipVerify.Shorthand != "" {
		t.Errorf("replay: --tls-skip-verify should have no shorthand, got -%s", tlsSkipVerify.Shorthand)
	}
}

// TestVersionVerboseFlag confirms --verbose is registered on the version command.
func TestVersionVerboseFlag(t *testing.T) {
	f := versionCmd.Flags().Lookup("verbose")
	if f == nil {
		t.Fatal("version: --verbose flag not found")
	}
	if f.Value.Type() != "bool" {
		t.Errorf("version: --verbose should be bool, got %s", f.Value.Type())
	}
}
