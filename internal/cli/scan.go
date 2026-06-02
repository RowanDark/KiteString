package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	ksout "github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/internal/scope"
	"github.com/RowanDark/kitestring/internal/wordlist"
	"github.com/RowanDark/kitestring/pkg/proute"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [url]",
	Short: "Context-aware API endpoint discovery",
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
		var targetStr string
		if len(args) > 0 && args[0] != "-" {
			targetStr = args[0]
		} else {
			sc := bufio.NewScanner(os.Stdin)
			if sc.Scan() {
				targetStr = strings.TrimSpace(sc.Text())
			}
			if err := sc.Err(); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		}
		if targetStr == "" {
			return fmt.Errorf("target URL required: pass as argument or pipe to stdin with -")
		}

		targets, err := proute.ParseTarget(targetStr)
		if err != nil {
			return fmt.Errorf("invalid target: %w", err)
		}

		// --- Scope setup ---
		scopeFile, _ := cmd.Flags().GetString("scope-file")
		scopePatterns, _ := cmd.Flags().GetStringArray("scope")
		excludePatterns, _ := cmd.Flags().GetStringArray("exclude")
		warnOutOfScope, _ := cmd.Flags().GetBool("warn-out-of-scope")

		var sc *scope.Scope
		if scopeFile != "" {
			sc, err = scope.LoadScope(scopeFile)
			if err != nil {
				return fmt.Errorf("scope file: %w", err)
			}
			sc = scope.New(append(sc.Includes(), scopePatterns...), append(sc.Excludes(), excludePatterns...))
		} else if len(scopePatterns) > 0 || len(excludePatterns) > 0 {
			sc = scope.New(scopePatterns, excludePatterns)
		}

		if sc != nil {
			filtered, skipped := sc.FilterTargets(targets)
			if skipped > 0 && warnOutOfScope {
				for _, t := range targets {
					if sc.IsOutOfScope(t.Host) {
						fmt.Fprintf(os.Stderr, "[WARN] skipping out-of-scope target: %s\n", t.Raw)
					}
				}
			}
			targets = filtered
			if !quiet {
				fmt.Fprintf(os.Stderr, "Scope: %d in-scope target(s), %d skipped\n",
					len(targets), skipped)
			}
		}

		// --- Wordlist loading ---
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

		// --- Build scan config ---
		config, buildErr := buildScanConfig(cmd)
		if buildErr != nil {
			return buildErr
		}
		if sc != nil {
			config.Scope = sc
		}

		// --- Run ---
		s, err := scan.New(config)
		if err != nil {
			return err
		}

		reportFmt, _ := cmd.Flags().GetString("report")
		var collector *ksout.CollectingWriter
		if reportFmt != "" {
			baseWriter, wErr := ksout.NewWriter(config.OutputFormat, nil)
			if wErr != nil {
				return wErr
			}
			collector = ksout.NewCollectingWriter(baseWriter)
			s.SetWriter(collector)
		}

		if !quiet {
			fmt.Fprintf(os.Stderr, "Scanning %d target(s) with %d routes...\n",
				len(targets), len(allRoutes))
		}

		if err := s.Run(targets, allRoutes); err != nil {
			return err
		}

		if !quiet {
			fmt.Fprintf(os.Stderr, "Found %d result(s).\n", s.ResultCount())
		}

		if reportFmt != "" && collector != nil {
			meta := ksout.ReportMeta{
				Target:    targetStr,
				ScanDate:  time.Now(),
				Wordlists: wordlistFiles,
				KSVersion: Version,
			}
			report := ksout.BuildReport(collector.Results(), meta)
			filename, wErr := writeAutoReport(report, reportFmt)
			if wErr != nil {
				return wErr
			}
			if !quiet {
				fmt.Fprintf(os.Stderr, "Report written to %s\n", filename)
			}
		}

		return nil
	},
}

func buildScanConfig(cmd *cobra.Command) (proute.ScanConfig, error) {
	threads, _ := cmd.Flags().GetInt("threads")
	parallelHosts, _ := cmd.Flags().GetInt("parallel-hosts")
	timeoutSec, _ := cmd.Flags().GetInt("timeout")
	delay, _ := cmd.Flags().GetDuration("delay")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	backoffBase, _ := cmd.Flags().GetDuration("backoff-base")
	backoffMax, _ := cmd.Flags().GetDuration("backoff-max")
	unreachableThreshold, _ := cmd.Flags().GetInt("unreachable-threshold")
	failCodes, _ := cmd.Flags().GetIntSlice("fail-status-codes")
	successCodes, _ := cmd.Flags().GetIntSlice("success-status-codes")
	ignoreLengthStrs, _ := cmd.Flags().GetStringArray("ignore-length")
	headerStrs, _ := cmd.Flags().GetStringArray("header")
	userAgent, _ := cmd.Flags().GetString("user-agent")
	followRedirects, _ := cmd.Flags().GetBool("follow-redirects")
	maxRedirects, _ := cmd.Flags().GetInt("max-redirects")
	disablePreflight, _ := cmd.Flags().GetBool("disable-precheck")
	preflightDepth, _ := cmd.Flags().GetInt("preflight-depth")
	quarantineThresh, _ := cmd.Flags().GetInt("quarantine-threshold")
	wildcardDetection, _ := cmd.Flags().GetBool("wildcard-detection")
	filterAPI, _ := cmd.Flags().GetString("filter-api")
	forceMethod, _ := cmd.Flags().GetString("force-method")
	blacklistDomains, _ := cmd.Flags().GetStringArray("blacklist-domain")
	similarityThreshold, _ := cmd.Flags().GetFloat64("similarity-threshold")
	disableSimilarity, _ := cmd.Flags().GetBool("disable-similarity")

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
		Timeout:              time.Duration(timeoutSec) * time.Second,
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
		OutputFormat:         output, // package-level var bound to -o/--output in root.go
		DisablePreflight:     disablePreflight,
		PreflightDepth:       preflightDepth,
		FilterAPIKSUID:       filterAPI,
		ForceMethod:          forceMethod,
		BlacklistDomains:     blacklistDomains,
		SimilarityThreshold:  similarityThreshold,
		DisableSimilarity:    disableSimilarity,
		Verbose:              verbose, // package-level var bound to -v/--verbose in root.go
	}, nil
}

func init() {
	// Wordlist & source flags
	scanCmd.Flags().StringArrayP("wordlist", "w", nil, "wordlist file(s) (.ks, .txt, or .json); repeatable")
	scanCmd.Flags().StringArrayP("wordlist-alias", "A", nil, "cached wordlist alias (e.g. apiroutes or apiroutes:20000); repeatable")
	scanCmd.Flags().Int("head", 0, "use only the first N routes from each wordlist (0 = all)")
	scanCmd.Flags().StringP("seclists", "S", "", "SecLists alias to fetch on demand (e.g. api-endpoints)")
	scanCmd.Flags().String("openapi-url", "", "fetch OpenAPI/Swagger spec from URL at scan time")
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
	scanCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")

	// Filter flags
	scanCmd.Flags().IntSlice("fail-status-codes", nil, "status codes to suppress (e.g. 404,403); comma-separated")
	scanCmd.Flags().IntSlice("success-status-codes", nil, "only report these status codes; comma-separated")
	scanCmd.Flags().StringArray("ignore-length", nil, "suppress responses at this content length or range (e.g. 1234 or 100-200); repeatable")

	// Request flags
	scanCmd.Flags().StringArrayP("header", "H", nil, "extra request header 'Key: Value'; repeatable")
	scanCmd.Flags().String("user-agent", "KiteString/1.0", "custom User-Agent string")
	scanCmd.Flags().BoolP("follow-redirects", "r", true, "follow HTTP redirects")
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

	// Scope flags
	scanCmd.Flags().String("scope-file", "", "path to scope file (# comments, ! prefix for excludes)")
	scanCmd.Flags().StringArray("scope", nil, "inline include pattern (e.g. *.example.com); repeatable")
	scanCmd.Flags().StringArray("exclude", nil, "inline exclude pattern (e.g. staging.example.com); repeatable")
	scanCmd.Flags().Bool("skip-out-of-scope", false, "silently skip out-of-scope targets (default when scope is defined)")
	scanCmd.Flags().Bool("warn-out-of-scope", false, "log a warning for each skipped out-of-scope target")

	// Misc
	scanCmd.Flags().IntP("depth", "d", 2, "crawl depth for context discovery")
	scanCmd.Flags().String("report", "", "auto-generate report on completion: md, markdown, html (writes ks-report-<timestamp>.<ext>)")
}
