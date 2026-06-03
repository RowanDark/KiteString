package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/RowanDark/kitestring/internal/config"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage named scan profiles",
	Long: `List, inspect, and create named scan profiles from the KiteString config file.

Profiles let you define scope, wordlists, rate limits, and output settings once
per bug bounty program and invoke them with a single --profile flag.

Examples:
  ks profile list
  ks profile show hackerone-stripe
  ks profile new my-program`,
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all defined profiles with key settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadActiveConfig()
		if err != nil {
			return err
		}

		names := cfg.ListProfiles()
		if len(names) == 0 {
			fmt.Fprintln(os.Stderr, "No profiles defined. Run 'ks profile new <name>' to create one.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PROFILE\tCONN/HOST\tDELAY\tOUTPUT\tWORDLISTS\tSCOPE FILE")
		for _, name := range names {
			p := cfg.Profiles[name]

			conn := "-"
			if p.MaxConnPerHost != nil {
				conn = fmt.Sprintf("%d", *p.MaxConnPerHost)
			}
			delay := "-"
			if p.Delay != nil {
				delay = p.Delay.String()
			}
			out := p.Output
			if out == "" {
				out = cfg.Defaults.Output
			}
			wlCount := fmt.Sprintf("%d", len(p.Wordlists))
			scope := p.ScopeFile
			if scope == "" {
				scope = "-"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", name, conn, delay, out, wlCount, scope)
		}
		return w.Flush()
	},
}

var profileShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show the fully resolved config for a profile (defaults merged in)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadActiveConfig()
		if err != nil {
			return err
		}

		name := args[0]
		pc, err := cfg.ApplyProfile(name)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "Profile:\t%s\n", name)
		fmt.Fprintf(w, "MaxConnPerHost:\t%d\n", pc.MaxConnPerHost)
		fmt.Fprintf(w, "MaxParallelHosts:\t%d\n", pc.MaxParallelHosts)
		fmt.Fprintf(w, "Timeout:\t%s\n", pc.Timeout)
		fmt.Fprintf(w, "Delay:\t%s\n", pc.Delay)
		fmt.Fprintf(w, "Output:\t%s\n", pc.Output)
		fmt.Fprintf(w, "UserAgent:\t%s\n", pc.UserAgent)
		codes := make([]string, len(pc.FailStatusCodes))
		for i, c := range pc.FailStatusCodes {
			codes[i] = fmt.Sprintf("%d", c)
		}
		fmt.Fprintf(w, "FailStatusCodes:\t[%s]\n", strings.Join(codes, ", "))
		fmt.Fprintf(w, "QuarantineThreshold:\t%d\n", pc.QuarantineThreshold)
		fmt.Fprintf(w, "SimilarityThreshold:\t%.2f\n", pc.SimilarityThreshold)
		if pc.ScopeFile != "" {
			fmt.Fprintf(w, "ScopeFile:\t%s\n", pc.ScopeFile)
		}
		if pc.OpenAPIURL != "" {
			fmt.Fprintf(w, "OpenAPIURL:\t%s\n", pc.OpenAPIURL)
		}
		if len(pc.Wordlists) > 0 {
			for i, wl := range pc.Wordlists {
				if i == 0 {
					fmt.Fprintf(w, "Wordlists:\t%s\n", wl)
				} else {
					fmt.Fprintf(w, "\t%s\n", wl)
				}
			}
		} else {
			fmt.Fprintf(w, "Wordlists:\t(none)\n")
		}
		if pc.Report != "" {
			fmt.Fprintf(w, "Report:\t%s\n", pc.Report)
		}
		return w.Flush()
	},
}

var profileNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Interactively create a new named profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		cfgPath := activeConfigPath()
		cfg, err := config.Load(cfgPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			cfg = config.Default()
		}

		if _, exists := cfg.Profiles[name]; exists {
			return fmt.Errorf("profile %q already exists; edit %s directly to modify it", name, cfgPath)
		}

		// Detect non-interactive stdin and fail with a helpful message.
		fi, _ := os.Stdin.Stat()
		if fi.Mode()&os.ModeCharDevice == 0 {
			return fmt.Errorf("'ks profile new' requires an interactive terminal; edit %s directly instead", cfgPath)
		}

		r := bufio.NewReader(os.Stdin)
		prompt := func(question, defaultVal string) string {
			if defaultVal != "" {
				fmt.Fprintf(os.Stderr, "  %s [%s]: ", question, defaultVal)
			} else {
				fmt.Fprintf(os.Stderr, "  %s: ", question)
			}
			line, _ := r.ReadString('\n')
			v := strings.TrimSpace(line)
			if v == "" {
				return defaultVal
			}
			return v
		}

		fmt.Fprintf(os.Stderr, "Creating profile %q — press Enter to accept defaults.\n\n", name)

		p := config.Profile{}

		if v := prompt("Scope file path (optional)", ""); v != "" {
			p.ScopeFile = v
		}
		if v := prompt("OpenAPI spec URL (optional)", ""); v != "" {
			p.OpenAPIURL = v
		}
		if v := prompt("Delay between requests (e.g. 200ms, 1s)", "0s"); v != "0s" && v != "" {
			if d, parseErr := time.ParseDuration(v); parseErr != nil {
				fmt.Fprintf(os.Stderr, "  [warn] invalid duration %q, skipping\n", v)
			} else {
				dd := config.Duration{Duration: d}
				p.Delay = &dd
			}
		}
		if v := prompt("Max connections per host", "5"); v != "" && v != "5" {
			var n int
			if _, err := fmt.Sscan(v, &n); err == nil {
				p.MaxConnPerHost = &n
			}
		}
		if v := prompt("Output format (pretty, text, jsonl)", "pretty"); v != "" && v != "pretty" {
			p.Output = v
		}
		if v := prompt("Report format (md, html, or empty for none)", ""); v != "" {
			p.Report = v
		}
		if v := prompt("Wordlists, comma-separated paths (optional)", ""); v != "" {
			for _, wl := range strings.Split(v, ",") {
				wl = strings.TrimSpace(wl)
				if wl != "" {
					p.Wordlists = append(p.Wordlists, wl)
				}
			}
		}

		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]config.Profile)
		}
		cfg.Profiles[name] = p

		if err := cfg.Save(cfgPath); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "\nProfile %q saved to %s\n", name, cfgPath)
		fmt.Fprintf(os.Stderr, "Use it with: ks scan <target> --profile %s\n", name)
		return nil
	},
}

// loadActiveConfig loads the config from the active config file path.
// Returns Default config if the file does not exist.
func loadActiveConfig() (*config.Config, error) {
	path := activeConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.Default(), nil
		}
		return nil, err
	}
	return cfg, nil
}

// activeConfigPath returns the config file path from --config flag or default.
func activeConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	home, _ := os.UserHomeDir()
	return home + "/.kitestring.yaml"
}

func init() {
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileShowCmd)
	profileCmd.AddCommand(profileNewCmd)
}
