package cli

import (
	"fmt"
	nethttp "net/http"
	"os"
	"time"

	"github.com/RowanDark/kitestring/internal/config"
	"github.com/RowanDark/kitestring/internal/input"
	ksoutput "github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/internal/recon"
	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/internal/scope"
	"github.com/RowanDark/kitestring/internal/wordlist"
	"github.com/RowanDark/kitestring/pkg/proute"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:     "scan [url]",
	Aliases: []string{"s"},
	Short:   "Context-aware API endpoint discovery",
	Long: `Scan a target URL for API endpoints using context-aware analysis.

KiteString sends routes with correct HTTP methods, headers, parameters, and
body content derived from the wordlist schema — not blind path fuzzing.

Examples:
  ks scan https://example.com -w routes.txt
  ks scan https://api.example.com/v1 -A apiroutes
  ks scan https://api.example.com -A apiroutes:20000
  ks scan https://api.example.com --openapi-url https://api.example.com/openapi.json
  ks scan - -w routes.txt   # read target URL from stdin`,
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

		// --- Scope filtering ---
		// Profile scope_file is used only when --scope-file not explicitly set.
		if activeProfile != nil && activeProfile.ScopeFile != "" && !cmd.Flags().Changed("scope-file") {
			if err := cmd.Flags().Set("scope-file", activeProfile.ScopeFile); err != nil {
				return fmt.Errorf("applying profile scope_file: %w", err)
			}
		}
		sc, scopeErr := buildScope(cmd)
		if scopeErr != nil {
			return scopeErr
		}
		if sc != nil {
			warnOOS, _ := cmd.Flags().GetBool("warn-out-of-scope")
			filtered, skipped := sc.FilterTargets(targets)
			if !quiet {
				fmt.Fprintf(os.Stderr, "Scope: %d in-scope target(s), %d skipped\n",
					len(filtered), skipped)
				if warnOOS {
					for _, t := range targets {
						if sc.IsOutOfScope(t.Host) {
							fmt.Fprintf(os.Stderr, "[warn] out-of-scope target skipped: %s\n", t.Host)
						}
					}
				}
			}
			targets = filtered
			if len(targets) == 0 {
				return fmt.Errorf("no in-scope targets remain after scope filtering")
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
		// Inject profile openapi_url when --openapi-url not explicitly provided.
		if activeProfile != nil && activeProfile.OpenAPIURL != "" && !cmd.Flags().Changed("openapi-url") {
			if err := cmd.Flags().Set("openapi-url", activeProfile.OpenAPIURL); err != nil {
				return fmt.Errorf("applying profile openapi_url: %w", err)
			}
		}

		aliases, _ := cmd.Flags().GetStringArray("wordlist-alias")
		wordlistFiles, _ := cmd.Flags().GetStringArray("wordlist")
		headN, _ := cmd.Flags().GetInt("head")
		seclistsAlias, _ := cmd.Flags().GetString("seclists")
		openapiURL, _ := cmd.Flags().GetString("openapi-url")
		openapiFile, _ := cmd.Flags().GetString("openapi-file")

		for _, spec := range aliases {
			path, limit, resolveErr := wordlist.ResolveAlias(spec)
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

		var allRoutes []proute.Route

		if len(wordlistFiles) > 0 {
			var loaded []proute.Route
			if headN > 0 {
				loaded, err = wordlist.Head(wordlistFiles, headN)
			} else {
				loaded, err = wordlist.Load(wordlistFiles)
			}
			if err != nil {
				return err
			}
			allRoutes = append(allRoutes, loaded...)
		}

		if openapiURL != "" {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Fetching OpenAPI spec from %s ...\n", openapiURL)
			}
			routes, fetchErr := wordlist.FetchFromURL(openapiURL)
			if fetchErr != nil {
				return fmt.Errorf("openapi-url: %w", fetchErr)
			}
			if !quiet {
				fmt.Fprintf(os.Stderr, "  Loaded %d routes from spec\n", len(routes))
			}
			allRoutes = append(allRoutes, routes...)
		}

		if openapiFile != "" {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Loading OpenAPI spec from %s ...\n", openapiFile)
			}
			routes, fetchErr := wordlist.FetchFromFile(openapiFile)
			if fetchErr != nil {
				return fmt.Errorf("openapi-file: %w", fetchErr)
			}
			if !quiet {
				fmt.Fprintf(os.Stderr, "  Loaded %d routes from spec\n", len(routes))
			}
			allRoutes = append(allRoutes, routes...)
		}

		if len(allRoutes) == 0 {
			return fmt.Errorf("no routes loaded: specify -w, -A, -S, or --openapi-url/--openapi-file")
		}

		// --- JS extraction pre-scan step ---
		jsExtract, _ := cmd.Flags().GetBool("js-extract")
		jsDepth, _ := cmd.Flags().GetInt("js-depth")
		if jsExtract {
			timeoutSec, _ := cmd.Flags().GetInt("timeout")
			jsClient := &nethttp.Client{
				Timeout: time.Duration(timeoutSec) * time.Second,
			}
			var jsInScope func(string) bool
			if sc != nil {
				jsInScope = sc.IsInScope
			}
			for _, target := range targets {
				jsRoutes, jsErr := recon.CrawlAndExtract(target, jsClient, jsDepth, jsInScope)
				if jsErr != nil {
					fmt.Fprintf(os.Stderr, "js-extract %s: %v\n", target.Host, jsErr)
					continue
				}
				if verbose == "debug" || verbose == "trace" {
					for _, r := range jsRoutes {
						fmt.Fprintf(os.Stderr, "[js-extract] %s %s (source: %s)\n",
							r.Method, r.Path, r.Source)
					}
				} else if !quiet {
					fmt.Fprintf(os.Stderr, "[js-extract] %s: %d route(s) extracted from JS\n",
						target.Host, len(jsRoutes))
				}
				allRoutes = append(allRoutes, jsRoutes...)
			}
		}

		// --- Build scan config ---
		config, buildErr := buildScanConfig(cmd, activeProfile)
		if buildErr != nil {
			return buildErr
		}

		// Wire scope check into the scan engine (redirect layer).
		if sc != nil {
			warnOOS, _ := cmd.Flags().GetBool("warn-out-of-scope")
			config.ScopeCheck = func(host string) bool {
				if sc.IsInScope(host) {
					return true
				}
				if warnOOS && !quiet {
					fmt.Fprintf(os.Stderr, "[warn] blocked out-of-scope redirect → %s\n", host)
				}
				return false
			}
		}

		// --- Run ---
		s, err := scan.New(config)
		if err != nil {
			return err
		}

		// Checkpoint / resume wiring.
		checkpointPath, _ := cmd.Flags().GetString("checkpoint")
		resumePath, _ := cmd.Flags().GetString("resume")
		checkpointInterval, _ := cmd.Flags().GetInt("checkpoint-interval")
		if resumePath != "" {
			checkpointPath = resumePath
		}
		if checkpointPath != "" {
			s.SetCheckpoint(checkpointPath, checkpointInterval, wordlistFiles)
		}

		if !quiet {
			fmt.Fprintf(os.Stderr, "Scanning %d target(s) with %d routes...\n",
				len(targets), len(allRoutes))
		}

		start := time.Now()
		if err := s.Run(targets, allRoutes); err != nil {
			return err
		}
		elapsed := time.Since(start)

		if !quiet {
			fmt.Fprintf(os.Stderr, "Found %d result(s).\n", s.ResultCount())
		}

		// Apply profile report format when --report not explicitly set.
		if activeProfile != nil && activeProfile.Report != "" && !cmd.Flags().Changed("report") {
			if err := cmd.Flags().Set("report", activeProfile.Report); err != nil {
				return fmt.Errorf("applying profile report: %w", err)
			}
		}
		reportFormat, _ := cmd.Flags().GetString("report")
		if reportFormat != "" {
			wordlistNames, _ := cmd.Flags().GetStringArray("wordlist")
			targetStr := ""
			if len(targets) > 0 {
				targetStr = targets[0].Host
			}
			meta := ksoutput.ReportMeta{
				Target:    targetStr,
				ScanDate:  time.Now(),
				Wordlists: wordlistNames,
				Duration:  elapsed,
				KSVersion: Version,
			}
			path, reportErr := writeAutoReport(s.Results(), meta, reportFormat)
			if reportErr != nil {
				fmt.Fprintf(os.Stderr, "[warn] report generation failed: %v\n", reportErr)
			} else if !quiet {
				fmt.Fprintf(os.Stderr, "Report written to %s\n", path)
			}
		}

		return nil
	},
}

// buildScope constructs a *scope.Scope from --scope-file, --scope, and --exclude flags.
// Returns nil when no scope flags are provided (no filtering).
func buildScope(cmd *cobra.Command) (*scope.Scope, error) {
	scopeFile, _ := cmd.Flags().GetString("scope-file")
	scopePatterns, _ := cmd.Flags().GetStringArray("scope")
	excludePatterns, _ := cmd.Flags().GetStringArray("exclude")

	if scopeFile == "" && len(scopePatterns) == 0 && len(excludePatterns) == 0 {
		return nil, nil
	}

	s := scope.New()
	if scopeFile != "" {
		if err := s.LoadFile(scopeFile); err != nil {
			return nil, fmt.Errorf("--scope-file: %w", err)
		}
	}
	for _, p := range scopePatterns {
		if err := s.AddInclude(p); err != nil {
			return nil, fmt.Errorf("--scope %q: %w", p, err)
		}
	}
	for _, p := range excludePatterns {
		if err := s.AddExclude(p); err != nil {
			return nil, fmt.Errorf("--exclude %q: %w", p, err)
		}
	}
	return s, nil
}

// buildScanConfig constructs a ScanConfig from CLI flags, applying profile values
// for any flag the user did not explicitly set (precedence: CLI > profile > defaults).
func buildScanConfig(cmd *cobra.Command, profile *config.ProbeConfig) (proute.ScanConfig, error) {
	getInt := func(name string, profileVal int) int {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetInt(name)
			return v
		}
		if profile != nil && profileVal != 0 {
			return profileVal
		}
		v, _ := cmd.Flags().GetInt(name)
		return v
	}
	getDuration := func(name string, profileVal time.Duration) time.Duration {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetDuration(name)
			return v
		}
		if profile != nil {
			return profileVal
		}
		v, _ := cmd.Flags().GetDuration(name)
		return v
	}
	getFloat64 := func(name string, profileVal float64) float64 {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetFloat64(name)
			return v
		}
		if profile != nil && profileVal != 0 {
			return profileVal
		}
		v, _ := cmd.Flags().GetFloat64(name)
		return v
	}
	getIntSlice := func(name string, profileVal []int) []int {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetIntSlice(name)
			return v
		}
		if profile != nil && len(profileVal) > 0 {
			return profileVal
		}
		v, _ := cmd.Flags().GetIntSlice(name)
		return v
	}
	getString := func(name string, profileVal string) string {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetString(name)
			return v
		}
		if profile != nil && profileVal != "" {
			return profileVal
		}
		v, _ := cmd.Flags().GetString(name)
		return v
	}

	var profileMaxConn, profileParallel, profileQuarantine int
	var profileTimeout, profileDelay time.Duration
	var profileSimilarity float64
	var profileFailCodes []int
	var profileUserAgent string
	if profile != nil {
		profileMaxConn = profile.MaxConnPerHost
		profileParallel = profile.MaxParallelHosts
		profileTimeout = profile.Timeout
		profileDelay = profile.Delay
		profileSimilarity = profile.SimilarityThreshold
		profileFailCodes = profile.FailStatusCodes
		profileQuarantine = profile.QuarantineThreshold
		profileUserAgent = profile.UserAgent
	}

	threads := getInt("threads", profileMaxConn)
	parallelHosts := getInt("parallel-hosts", profileParallel)
	timeoutSec := getInt("timeout", 0)
	var timeout time.Duration
	if profile != nil && !cmd.Flags().Changed("timeout") && profileTimeout != 0 {
		timeout = profileTimeout
	} else {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	delay := getDuration("delay", profileDelay)
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	backoffBase, _ := cmd.Flags().GetDuration("backoff-base")
	backoffMax, _ := cmd.Flags().GetDuration("backoff-max")
	unreachableThreshold, _ := cmd.Flags().GetInt("unreachable-threshold")
	failCodes := getIntSlice("fail-status-codes", profileFailCodes)
	successCodes, _ := cmd.Flags().GetIntSlice("success-status-codes")
	ignoreLengthStrs, _ := cmd.Flags().GetStringArray("ignore-length")
	headerStrs, _ := cmd.Flags().GetStringArray("header")
	userAgent := getString("user-agent", profileUserAgent)
	followRedirects, _ := cmd.Flags().GetBool("follow-redirects")
	maxRedirects, _ := cmd.Flags().GetInt("max-redirects")
	disablePreflight, _ := cmd.Flags().GetBool("disable-precheck")
	preflightDepth, _ := cmd.Flags().GetInt("preflight-depth")
	quarantineThresh := getInt("quarantine-threshold", profileQuarantine)
	wildcardDetection, _ := cmd.Flags().GetBool("wildcard-detection")
	filterAPI, _ := cmd.Flags().GetString("filter-api")
	forceMethod, _ := cmd.Flags().GetString("force-method")
	blacklistDomains, _ := cmd.Flags().GetStringArray("blacklist-domain")
	similarityThreshold := getFloat64("similarity-threshold", profileSimilarity)
	disableSimilarity, _ := cmd.Flags().GetBool("disable-similarity")

	// Apply profile output format to the package-level output var when -o not set.
	if profile != nil && profile.Output != "" && !cmd.Flags().Changed("output") {
		output = profile.Output
	}

	if !followRedirects {
		maxRedirects = 0
	}

	var ignoreLengths []proute.LengthRange
	for _, s := range ignoreLengthStrs {
		lr, err := proute.ParseLengthRange(s)
		if err != nil {
			return proute.ScanConfig{}, fmt.Errorf("--ignore-length %q: %w", s, err)
		}
		ignoreLengths = append(ignoreLengths, lr)
	}

	return proute.ScanConfig{
		MaxConnPerHost:       threads,
		MaxParallelHosts:     parallelHosts,
		Timeout:              timeout,
		Delay:                delay,
		MaxRetries:           maxRetries,
		BackoffBase:          backoffBase,
		BackoffMax:           backoffMax,
		UnreachableThreshold: unreachableThreshold,
		FailStatusCodes:      failCodes,
		SuccessStatusCodes:   successCodes,
		IgnoreLengths:        ignoreLengths,
		Headers:              headerStrs,
		UserAgent:            userAgent,
		MaxRedirects:         maxRedirects,
		WildcardDetection:    wildcardDetection,
		QuarantineThresh:     quarantineThresh,
		OutputFormat:         output,
		DisablePreflight:     disablePreflight,
		PreflightDepth:       preflightDepth,
		FilterAPIKSUID:       filterAPI,
		ForceMethod:          forceMethod,
		BlacklistDomains:     blacklistDomains,
		SimilarityThreshold:  similarityThreshold,
		DisableSimilarity:    disableSimilarity,
		Verbose:              verbose,
	}, nil
}

func init() {
	// Wordlist & source flags
	scanCmd.Flags().StringArrayP("wordlist", "w", nil, "wordlist file(s) (.ks, .txt, or .json); repeatable")
	scanCmd.Flags().StringArrayP("wordlist-alias", "A", nil, "cached wordlist alias (e.g. apiroutes or apiroutes:20000); repeatable")
	scanCmd.Flags().Int("head", 0, "use only the first N routes from each wordlist (0 = all)")
	scanCmd.Flags().StringP("seclists", "S", "", "SecLists alias to fetch on demand (e.g. api-endpoints)")
	scanCmd.Flags().StringP("openapi-url", "O", "", "fetch OpenAPI/Swagger spec from URL at scan time")
	scanCmd.Flags().String("openapi-file", "", "load local OpenAPI/Swagger spec file at scan time")

	// Connection & timing flags
	scanCmd.Flags().IntP("threads", "t", 10, "concurrent connections per host")
	scanCmd.Flags().IntP("parallel-hosts", "j", 10, "maximum number of hosts to scan concurrently")
	scanCmd.Flags().Int("timeout", 10, "request timeout in seconds")
	scanCmd.Flags().Duration("delay", 0, "fixed inter-request delay per host (e.g. 200ms, 1s)")
	scanCmd.Flags().Int("max-retries", 3, "maximum retries on 429 or connection failure")
	scanCmd.Flags().Duration("backoff-base", 5*time.Second, "base duration for exponential backoff on 429")
	scanCmd.Flags().Duration("backoff-max", 60*time.Second, "maximum backoff ceiling")
	scanCmd.Flags().Int("unreachable-threshold", 5, "consecutive connection failures before marking a host unreachable")
	scanCmd.Flags().StringP("proxy", "P", "", "HTTP proxy URL")

	// Filter flags
	scanCmd.Flags().IntSlice("fail-status-codes", nil, "status codes to suppress (e.g. 404,403); comma-separated")
	scanCmd.Flags().IntSlice("success-status-codes", nil, "only report these status codes; comma-separated")
	scanCmd.Flags().StringArray("ignore-length", nil, "suppress responses at this content length or range (e.g. 1234 or 100-200); repeatable")

	// Request flags
	scanCmd.Flags().StringArrayP("header", "H", nil, "extra request header 'Key: Value'; repeatable")
	scanCmd.Flags().String("user-agent", "KiteString/1.0", "custom User-Agent string")
	scanCmd.Flags().Bool("follow-redirects", true, "follow HTTP redirects")
	scanCmd.Flags().Int("max-redirects", 3, "maximum redirects to follow (when --follow-redirects is true)")
	scanCmd.Flags().StringArray("blacklist-domain", nil, "do not follow redirects to these domains; repeatable")
	scanCmd.Flags().String("force-method", "", "override HTTP method for all routes")

	// Preflight & wildcard flags
	scanCmd.Flags().Bool("disable-precheck", false, "skip preflight host check and wildcard baseline building")
	scanCmd.Flags().Int("preflight-depth", 1, "path depth for wildcard baseline probing")
	scanCmd.Flags().Int("quarantine-threshold", 10, "consecutive wildcard responses before host quarantine")
	scanCmd.Flags().Bool("wildcard-detection", true, "detect and quarantine wildcard routing hosts")
	scanCmd.Flags().Bool("kitebuilder-full-scan", false, "send all routes regardless of wildcard baseline")

	// API-mode flags
	scanCmd.Flags().String("filter-api", "", "only scan routes matching this KSUID")

	// Similarity filtering flags
	scanCmd.Flags().Float64("similarity-threshold", 0.85, "body similarity threshold for suppressing templated responses (0.0–1.0)")
	scanCmd.Flags().Bool("disable-similarity", false, "skip body similarity scoring (faster, but may produce false positives on templated 200 responses)")

	// JS extraction flags
	scanCmd.Flags().BoolP("js-extract", "J", false, "fetch root page, parse <script> tags, and add extracted routes to scan queue")
	scanCmd.Flags().Int("js-depth", 1, "pages deep to crawl looking for script tags (1 = root page only)")

	// Scope flags
	scanCmd.Flags().StringP("scope-file", "s", "", "path to scope file (lines: *.example.com, !exclude.com, 192.168.1.0/24)")
	scanCmd.Flags().StringArray("scope", nil, "inline include pattern (e.g. *.example.com); repeatable")
	scanCmd.Flags().StringArray("exclude", nil, "inline exclude pattern (e.g. staging.example.com); repeatable")
	scanCmd.Flags().Bool("skip-out-of-scope", false, "silently skip out-of-scope targets (default when scope is defined)")
	scanCmd.Flags().Bool("warn-out-of-scope", false, "log a warning for each out-of-scope target or redirect instead of silently skipping")

	// Report generation
	scanCmd.Flags().StringP("report", "R", "", "auto-generate report on completion: md, markdown, or html")

	// Checkpoint / resume flags
	scanCmd.Flags().StringP("checkpoint", "c", "", "path to checkpoint file; creates a new scan or resumes an existing one")
	scanCmd.Flags().StringP("resume", "r", "", "alias for --checkpoint that explicitly signals resume intent")
	scanCmd.Flags().Int("checkpoint-interval", 500, "save checkpoint every N completed requests")

	// Misc
	scanCmd.Flags().IntP("depth", "d", 2, "crawl depth for context discovery")

	// Profile
	scanCmd.Flags().StringP("profile", "p", "", "load settings from a named profile in the config file (~/.kitestring.yaml)")
}
