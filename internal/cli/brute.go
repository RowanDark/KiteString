package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RowanDark/kitestring/internal/brute"
	"github.com/RowanDark/kitestring/internal/config"
	"github.com/RowanDark/kitestring/internal/input"
	ksoutput "github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/internal/wordlist"
	"github.com/RowanDark/kitestring/pkg/proute"
	"github.com/spf13/cobra"
)

var bruteCmd = &cobra.Command{
	Use:   "brute [url]",
	Short: "Traditional path/directory fuzzing",
	Long: `Brute-force paths and directories against a target URL using a wordlist.

Unlike 'ks scan', brute mode does not perform context analysis — it sends
GET requests for each entry in the wordlist and reports responses based on
configured status code filters.

All wordlist sources (-w, -A, -S) and global scan flags are supported.

Examples:
  ks brute https://example.com -w wordlists/common.txt -e php,html
  ks brute https://api.example.com/v1 -w dirsearch.txt -D -e php,aspx
  ks brute https://example.com -S raft-medium-words`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// --- Target ---
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		if arg == "" {
			fi, _ := os.Stdin.Stat()
			if fi.Mode()&os.ModeCharDevice != 0 {
				return fmt.Errorf("target URL required: pass as argument or pipe to stdin with -")
			}
		}

		targets, err := input.ReadTargets(arg, os.Stdin)
		if err != nil {
			return fmt.Errorf("reading targets: %w", err)
		}
		if len(targets) == 0 {
			return fmt.Errorf("no targets found in input")
		}

		// --- Profile loading ---
		var activeProfile *config.ProbeConfig
		profileName, _ := cmd.Flags().GetString("profile")
		if profileName != "" {
			cfg, cfgErr := loadActiveConfig()
			if cfgErr != nil {
				return fmt.Errorf("loading config for --profile: %w", cfgErr)
			}
			pc, profErr := cfg.ApplyProfile(profileName)
			if profErr != nil {
				return profErr
			}
			activeProfile = pc
			if !quiet {
				fmt.Fprintf(os.Stderr, "Using profile: %s\n", profileName)
			}
		}

		// --- Wordlist loading ---
		// Inject profile wordlists when -w was not explicitly provided.
		if activeProfile != nil && len(activeProfile.Wordlists) > 0 && !cmd.Flags().Changed("wordlist") {
			for _, wl := range activeProfile.Wordlists {
				if err := cmd.Flags().Set("wordlist", wl); err != nil {
					return fmt.Errorf("applying profile wordlist: %w", err)
				}
			}
		}
		wordlistFiles, _ := cmd.Flags().GetStringArray("wordlist")
		headN, _ := cmd.Flags().GetInt("head")
		seclistsAlias, _ := cmd.Flags().GetString("seclists")
		wordlistAlias, _ := cmd.Flags().GetString("wordlist-alias")

		if wordlistAlias != "" {
			path, limit, resolveErr := wordlist.ResolveAlias(wordlistAlias)
			if resolveErr != nil {
				return resolveErr
			}
			wordlistFiles = append(wordlistFiles, path)
			if limit > 0 && headN == 0 {
				headN = limit
			}
		}

		if seclistsAlias != "" {
			path, resolveErr := wordlist.ResolveSecList(seclistsAlias)
			if resolveErr != nil {
				return resolveErr
			}
			wordlistFiles = append(wordlistFiles, path)
		}

		if len(wordlistFiles) == 0 {
			return fmt.Errorf("no wordlist specified: use -w, -A, or -S")
		}

		var allRoutes []proute.Route
		if headN > 0 {
			allRoutes, err = wordlist.Head(wordlistFiles, headN)
		} else {
			allRoutes, err = wordlist.Load(wordlistFiles)
		}
		if err != nil {
			return err
		}

		// Extract flat paths from routes (method/param context ignored in brute mode).
		rawPaths := make([]string, len(allRoutes))
		for i, r := range allRoutes {
			rawPaths[i] = r.Path
		}

		// --- Extension expansion ---
		extStr, _ := cmd.Flags().GetString("extensions")
		dirsearchCompat, _ := cmd.Flags().GetBool("dirsearch-compat")

		var extensions []string
		if extStr != "" {
			for _, e := range strings.Split(extStr, ",") {
				e = strings.TrimSpace(e)
				if e != "" {
					extensions = append(extensions, e)
				}
			}
		}

		var paths []string
		if dirsearchCompat && len(extensions) > 0 {
			paths = brute.ExpandDirsearch(rawPaths, extensions)
		} else {
			paths = rawPaths
			if len(extensions) > 0 {
				paths = brute.ExpandExtensions(rawPaths, extensions)
			}
		}
		paths = brute.Deduplicate(paths)

		if len(paths) == 0 {
			return fmt.Errorf("no paths to scan after expansion")
		}

		// --- Build scan config ---
		config, buildErr := buildScanConfig(cmd, activeProfile)
		if buildErr != nil {
			return buildErr
		}

		// --- Run ---
		b, err := brute.New(config)
		if err != nil {
			return err
		}

		if !quiet {
			fmt.Fprintf(os.Stderr, "Brute-forcing %d target(s) with %d path(s)...\n",
				len(targets), len(paths))
		}

		start := time.Now()
		if err := b.Run(targets, paths); err != nil {
			return err
		}
		elapsed := time.Since(start)

		if !quiet {
			fmt.Fprintf(os.Stderr, "Found %d result(s).\n", b.ResultCount())
		}

		// Apply profile report format when --report not explicitly set.
		if activeProfile != nil && activeProfile.Report != "" && !cmd.Flags().Changed("report") {
			if err := cmd.Flags().Set("report", activeProfile.Report); err != nil {
				return fmt.Errorf("applying profile report: %w", err)
			}
		}
		reportFormat, _ := cmd.Flags().GetString("report")
		if reportFormat != "" {
			targetStr := ""
			if len(targets) > 0 {
				targetStr = targets[0].Host
			}
			meta := ksoutput.ReportMeta{
				Target:    targetStr,
				ScanDate:  time.Now(),
				Wordlists: wordlistFiles,
				Duration:  elapsed,
				KSVersion: Version,
			}
			path, reportErr := writeAutoReport(b.Results(), meta, reportFormat)
			if reportErr != nil {
				fmt.Fprintf(os.Stderr, "[warn] report generation failed: %v\n", reportErr)
			} else if !quiet {
				fmt.Fprintf(os.Stderr, "Report written to %s\n", path)
			}
		}

		return nil
	},
}

func init() {
	// Wordlist flags
	bruteCmd.Flags().StringArrayP("wordlist", "w", nil, "wordlist file(s) (.ks, .txt, or .json); repeatable")
	bruteCmd.Flags().StringP("wordlist-alias", "A", "", "cached wordlist alias (e.g. apiroutes or apiroutes:20000)")
	bruteCmd.Flags().Int("head", 0, "use only the first N paths from each wordlist (0 = all)")
	bruteCmd.Flags().StringP("seclists", "S", "", "SecLists alias to fetch on demand (e.g. raft-medium-words)")

	// Extension flags
	bruteCmd.Flags().StringP("extensions", "e", "", "extensions to append to each path, comma-separated (e.g. php,json,aspx)")
	bruteCmd.Flags().BoolP("dirsearch-compat", "D", false, "substitute %%EXT%% placeholder instead of appending extensions")

	// Connection & timing flags
	bruteCmd.Flags().IntP("threads", "t", 40, "concurrent connections per host")
	bruteCmd.Flags().IntP("parallel-hosts", "j", 10, "maximum number of hosts to scan concurrently")
	bruteCmd.Flags().Int("timeout", 10, "request timeout in seconds")
	bruteCmd.Flags().Duration("delay", 0, "fixed inter-request delay per host (e.g. 200ms, 1s)")
	bruteCmd.Flags().Int("max-retries", 3, "maximum retries on 429 or connection failure")
	bruteCmd.Flags().Duration("backoff-base", 5*time.Second, "base duration for exponential backoff on 429")
	bruteCmd.Flags().Duration("backoff-max", 60*time.Second, "maximum backoff ceiling")
	bruteCmd.Flags().Int("unreachable-threshold", 5, "consecutive connection failures before marking a host unreachable")
	bruteCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")

	// Filter flags
	bruteCmd.Flags().IntSlice("fail-status-codes", nil, "status codes to suppress (e.g. 404,403); comma-separated")
	bruteCmd.Flags().IntSlice("success-status-codes", nil, "only report these status codes; comma-separated")
	bruteCmd.Flags().StringArray("ignore-length", nil, "suppress responses at this content length or range (e.g. 1234 or 100-200); repeatable")

	// Request flags
	bruteCmd.Flags().StringArrayP("header", "H", nil, "extra request header 'Key: Value'; repeatable")
	bruteCmd.Flags().String("user-agent", "KiteString/1.0", "custom User-Agent string")
	bruteCmd.Flags().BoolP("follow-redirects", "r", false, "follow HTTP redirects")
	bruteCmd.Flags().Int("max-redirects", 3, "maximum redirects to follow (when --follow-redirects is true)")
	bruteCmd.Flags().StringArray("blacklist-domain", nil, "do not follow redirects to these domains; repeatable")
	bruteCmd.Flags().String("force-method", "GET", "override HTTP method (default GET)")

	// Preflight & wildcard flags
	bruteCmd.Flags().Bool("disable-precheck", false, "skip preflight host check and wildcard baseline building")
	bruteCmd.Flags().IntP("preflight-depth", "d", 0, "directory depth for wildcard baseline probing (default 0 for brute mode)")
	bruteCmd.Flags().Int("quarantine-threshold", 10, "consecutive wildcard responses before host quarantine")
	bruteCmd.Flags().Bool("wildcard-detection", true, "detect and quarantine wildcard routing hosts")

	// Report generation
	bruteCmd.Flags().String("report", "", "auto-generate report on completion: md, markdown, or html")

	// Misc
	bruteCmd.Flags().String("filter-api", "", "only report routes matching this KSUID")

	// Profile
	bruteCmd.Flags().String("profile", "", "load settings from a named profile in the config file (~/.kitestring.yaml)")
}
