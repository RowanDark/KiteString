package cli

import (
	"bufio"
	"fmt"
	nethttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/RowanDark/kitestring/internal/replay"
)

var replayCmd = &cobra.Command{
	Use:     "replay [result-line]",
	Aliases: []string{"r"},
	Short:   "Reconstruct and re-execute a captured HTTP request",
	Long: `Replay takes a single result line from KiteString scan output (pretty, text, or
JSONL format) and reconstructs the original HTTP request, optionally forwarding
it through a proxy like Burp Suite or OWASP ZAP.

The result line can be provided as a positional argument, read from stdin when
'-' is passed or when stdin is not a TTY (pipe mode).

When a wordlist is provided alongside a KSUID, replay reconstructs the full
request with the original route's parameter schema (query params, headers, and
body). Without a matching KSUID the request is reconstructed from the URL and
method alone.

Examples:
  ks replay "200  POST    1337      42ms        https://target.com/api/v1/users"
  ks replay '{"method":"POST","url":"https://target.com/api/v1/users","ksuid":"abc123"}' \
      -w routes.ks --proxy http://localhost:8080

  # Pipe scan output directly into replay
  ks scan target.com -w routes.ks -o text | ks replay - --proxy http://localhost:8080

  # Show the reconstructed request before sending
  ks replay "200  GET  512  18ms  https://example.com/api/health" --show-request`,

	Args: cobra.MaximumNArgs(1),
	RunE: runReplay,
}

func runReplay(cmd *cobra.Command, args []string) error {
	wordlistFiles, _ := cmd.Flags().GetStringArray("wordlist")
	proxyURL, _ := cmd.Flags().GetString("proxy")
	tlsSkipVerify, _ := cmd.Flags().GetBool("tls-skip-verify")
	showRequest, _ := cmd.Flags().GetBool("show-request")
	showResponse, _ := cmd.Flags().GetBool("show-response")

	// Collect result lines to replay.
	lines, err := collectInputLines(cmd, args)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("no result lines to replay")
	}

	// Build the HTTP client (with proxy if requested).
	var client *nethttp.Client
	if proxyURL != "" {
		client, err = replay.NewProxyClient(proxyURL, tlsSkipVerify)
		if err != nil {
			return fmt.Errorf("proxy: %w", err)
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "Using proxy: %s\n", proxyURL)
		}
	} else {
		client = &nethttp.Client{Timeout: 30 * time.Second}
	}

	for _, line := range lines {
		if err := replayLine(line, wordlistFiles, client, showRequest, showResponse); err != nil {
			fmt.Fprintf(os.Stderr, "[error] %v\n", err)
		}
	}
	return nil
}

// collectInputLines gathers result lines from the positional argument or stdin.
func collectInputLines(cmd *cobra.Command, args []string) ([]string, error) {
	// Explicit argument that isn't '-': treat as a single inline result line.
	if len(args) == 1 && args[0] != "-" {
		return []string{args[0]}, nil
	}

	// '-' argument or no argument when stdin is not a TTY: read from stdin.
	readStdin := len(args) == 0 || args[0] == "-"
	if !readStdin {
		return nil, fmt.Errorf("unexpected argument: %q", args[0])
	}

	fi, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat stdin: %w", err)
	}
	if fi.Mode()&os.ModeCharDevice != 0 && len(args) == 0 {
		return nil, fmt.Errorf("result line required: pass as argument or pipe via stdin")
	}

	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		l := strings.TrimSpace(scanner.Text())
		if l != "" {
			lines = append(lines, l)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return lines, nil
}

func replayLine(line string, wordlistFiles []string, client *nethttp.Client, showRequest, showResponse bool) error {
	rr, err := replay.ParseResultLine(line)
	if err != nil {
		return fmt.Errorf("parse result line: %w", err)
	}
	rr.WordlistPaths = wordlistFiles

	if verbose == "info" || verbose == "debug" || verbose == "trace" {
		source := "URL+method"
		if rr.KSUID != "" && len(wordlistFiles) > 0 {
			source = fmt.Sprintf("KSUID %s", rr.KSUID)
		}
		fmt.Fprintf(os.Stderr, "[replay] %s %s (reconstructing from %s)\n",
			rr.Method, rr.URL, source)
	}

	resp, err := rr.Execute(client)
	if err != nil {
		return fmt.Errorf("execute %s %s: %w", rr.Method, rr.URL, err)
	}

	if showRequest {
		fmt.Fprintf(os.Stdout, "--- Request ---\n%s\n", resp.RawRequest)
	}

	fmt.Fprintf(os.Stdout, "%d  %s  %d bytes  %s\n",
		resp.StatusCode,
		rr.URL,
		len(resp.Body),
		resp.Duration.Round(time.Millisecond),
	)

	if showResponse && len(resp.Body) > 0 {
		fmt.Fprintf(os.Stdout, "--- Response ---\n%s\n", resp.Body)
	}

	return nil
}

func init() {
	replayCmd.Flags().StringArrayP("wordlist", "w", nil,
		"wordlist file(s) for KSUID lookup and parameter reconstruction (.ks, .txt, .json); repeatable")
	replayCmd.Flags().StringP("proxy", "P", "",
		"proxy URL to forward the request through (e.g. http://localhost:8080, socks5://127.0.0.1:1080)")
	replayCmd.Flags().Bool("tls-skip-verify", false,
		"skip TLS certificate verification (required for Burp Suite interception)")
	replayCmd.Flags().BoolP("show-request", "Q", false,
		"print the raw reconstructed HTTP request before sending")
	replayCmd.Flags().BoolP("show-response", "Z", false,
		"print the full response body after receiving")
}
