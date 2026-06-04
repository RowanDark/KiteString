package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/RowanDark/kitestring/internal/wordlist"
	"github.com/RowanDark/kitestring/pkg/ksfile"
	"github.com/RowanDark/kitestring/pkg/proute"
)

var wordlistSeclistsCmd = &cobra.Command{
	Use:     "seclists",
	Aliases: []string{"sl"},
	Short:   "Fetch and compile SecLists wordlists",
	Long: `Fetch wordlists from the SecLists GitHub repository and compile them
to .ks format for use with ks scan and ks brute.`,
}

var wordlistSeclistsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Print available SecLists aliases",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries := wordlist.ListSecListAliases()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ALIAS\tSECLISTS PATH")
		fmt.Fprintln(w, "-----\t-------------")
		for _, e := range entries {
			fmt.Fprintf(w, "%s\t%s\n", e.Alias, e.RepoPath)
		}
		return w.Flush()
	},
}

var wordlistSeclistsFetchCmd = &cobra.Command{
	Use:   "fetch [alias]",
	Short: "Fetch and cache a SecLists wordlist",
	Long: `Download a SecLists wordlist by alias and compile it to .ks format in
~/.cache/kitestring/wordlists/ as sl-<alias>.ks.

Use --all to fetch every defined alias.

Examples:
  ks wordlist seclists fetch api-endpoints
  ks wordlist seclists fetch --all`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")

		if all {
			for _, e := range wordlist.ListSecListAliases() {
				fmt.Printf("Fetching %s ...\n", e.Alias)
				path, err := wordlist.ResolveSecList(e.Alias)
				if err != nil {
					return err
				}
				fmt.Printf("  Saved → %s\n", path)
			}
			return nil
		}

		if len(args) == 0 {
			return fmt.Errorf("provide an alias or use --all; run: ks wordlist seclists list")
		}

		alias := args[0]
		fmt.Printf("Fetching %s ...\n", alias)
		path, err := wordlist.ResolveSecList(alias)
		if err != nil {
			return err
		}
		fmt.Printf("  Saved → %s\n", path)
		return nil
	},
}

var wordlistCmd = &cobra.Command{
	Use:     "wordlist",
	Aliases: []string{"wl", "w"},
	Short:   "Wordlist management",
	Long: `Manage KiteString wordlists (.ks format).

Subcommands allow listing available wordlists, pulling the latest curated
lists from GitHub, and compiling plain .txt or .json files into the
optimized .ks binary format.`,
}

var wordlistListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "l"},
	Short:   "List available wordlists",
	Long: `List all wordlists available on the CDN and their local cached status.

Displays alias, route count, compressed size, and whether the wordlist is
already cached in ~/.cache/kitestring/wordlists/.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		remote, err := wordlist.ListRemote()
		if err != nil {
			return fmt.Errorf("fetching manifest: %w", err)
		}
		cached, err := wordlist.ListCached()
		if err != nil {
			return fmt.Errorf("reading cache: %w", err)
		}

		cachedSet := make(map[string]bool, len(cached))
		for _, c := range cached {
			cachedSet[c.Alias] = true
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ALIAS\tROUTES\tSIZE (MB)\tCACHED\tDESCRIPTION")
		fmt.Fprintln(w, "-----\t------\t---------\t------\t-----------")
		for _, e := range remote {
			status := "no"
			if cachedSet[e.Alias] {
				status = "yes"
			}
			fmt.Fprintf(w, "%s\t%d\t%.1f\t%s\t%s\n",
				e.Alias, e.Count, e.CompressedSizeMB, status, e.Description)
		}
		w.Flush()
		return nil
	},
}

var wordlistUpdateCmd = &cobra.Command{
	Use:     "update [alias...]",
	Aliases: []string{"up", "u"},
	Short:   "Pull latest .ks wordlists from GitHub CDN",
	Long: `Download curated .ks wordlist files from the KiteString GitHub release CDN.

Without arguments all manifest entries are downloaded. Pass one or more alias
names to download specific lists.  Already-cached files are skipped unless
--force is provided.

Examples:
  ks wordlist update
  ks wordlist update apiroutes
  ks wordlist update apiroutes graphql --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		return wordlist.Update(args, force)
	},
}

var wordlistCompileCmd = &cobra.Command{
	Use:     "compile [file]",
	Aliases: []string{"c"},
	Short:   "Compile .txt or .json into .ks format",
	Long: `Compile a plain-text (.txt) or JSON (.json) wordlist into the optimized
.ks binary format for faster loading and deduplication.

Example:
  ks wordlist compile mylist.txt
  ks wordlist compile mylist.json --output mylist.ks`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks wordlist compile: not yet implemented")
		return nil
	},
}

// ---------- openapi subcommand tree ----------

var wordlistOpenAPICmd = &cobra.Command{
	Use:     "openapi",
	Aliases: []string{"oa"},
	Short:   "Ingest live OpenAPI/Swagger specs",
	Long: `Fetch and compile OpenAPI/Swagger specs into .ks wordlists, or search the
APIs.guru catalogue for publicly available specs.`,
}

var wordlistOpenAPIFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch and compile an OpenAPI spec to .ks",
	Long: `Download (or read locally) an OpenAPI/Swagger spec and compile it to a .ks
wordlist file containing structured routes with parameter metadata.

Examples:
  ks wordlist openapi fetch --url https://petstore.swagger.io/v2/swagger.json
  ks wordlist openapi fetch --url https://petstore.swagger.io/v2/swagger.json --output petstore.ks
  ks wordlist openapi fetch --file ./my-api.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		specURL, _ := cmd.Flags().GetString("url")
		specFile, _ := cmd.Flags().GetString("file")
		output, _ := cmd.Flags().GetString("output")

		if specURL == "" && specFile == "" {
			return fmt.Errorf("provide --url <spec-url> or --file <local-spec>")
		}
		if specURL != "" && specFile != "" {
			return fmt.Errorf("provide either --url or --file, not both")
		}

		var routes []proute.Route
		var source string

		if specURL != "" {
			fmt.Printf("Fetching spec from %s ...\n", specURL)
			var err error
			routes, err = wordlist.FetchFromURL(specURL)
			if err != nil {
				return err
			}
			source = specURL
			if output == "" {
				output = openapiDefaultOutput(specURL)
			}
		} else {
			fmt.Printf("Compiling spec from %s ...\n", specFile)
			var err error
			routes, err = wordlist.FetchFromFile(specFile)
			if err != nil {
				return err
			}
			source = specFile
			if output == "" {
				output = strings.TrimSuffix(specFile, filepath.Ext(specFile)) + ".ks"
			}
		}

		kf := ksfile.FromRoutes(routes, ksfile.KSFileMeta{
			Name:        filepath.Base(strings.TrimSuffix(output, ".ks")),
			Description: "OpenAPI spec: " + source,
			Source:      source,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		})
		if err := ksfile.Write(output, kf); err != nil {
			return fmt.Errorf("write .ks: %w", err)
		}
		fmt.Printf("  %d routes → %s\n", len(routes), output)
		return nil
	},
}

var wordlistOpenAPISearchCmd = &cobra.Command{
	Use:   "search <name>",
	Short: "Search APIs.guru for publicly available specs",
	Long: `Query the APIs.guru catalogue (https://apis.guru) and list entries whose
name contains the search term (case-insensitive).

Example:
  ks wordlist openapi search stripe
  ks wordlist openapi search github`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filter := args[0]
		fmt.Printf("Searching APIs.guru for %q ...\n", filter)

		entries, err := wordlist.ListAPIsGuru(filter)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Printf("No results found for %q\n", filter)
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTITLE\tVERSION\tSPEC URL")
		fmt.Fprintln(w, "----\t-----\t-------\t--------")
		for _, e := range entries {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.Title, e.Version, e.SpecURL)
		}
		return w.Flush()
	},
}

// openapiDefaultOutput derives a .ks output filename from a spec URL.
func openapiDefaultOutput(specURL string) string {
	base := filepath.Base(specURL)
	// Strip known spec extensions.
	for _, ext := range []string{".json", ".yaml", ".yml"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	if base == "" || base == "." {
		base = "openapi"
	}
	return base + ".ks"
}

func init() {
	wordlistUpdateCmd.Flags().BoolP("force", "f", false, "re-download all wordlists regardless of local state")

	wordlistCompileCmd.Flags().StringP("output", "o", "", "output .ks file path (default: <input>.ks)")

	wordlistSeclistsFetchCmd.Flags().Bool("all", false, "fetch and compile all defined SecLists aliases")

	wordlistOpenAPIFetchCmd.Flags().String("url", "", "URL of an OpenAPI/Swagger JSON or YAML spec")
	wordlistOpenAPIFetchCmd.Flags().String("file", "", "path to a local OpenAPI/Swagger spec file")
	wordlistOpenAPIFetchCmd.Flags().StringP("output", "o", "", "output .ks file path (default: derived from spec name)")

	wordlistOpenAPICmd.AddCommand(wordlistOpenAPIFetchCmd)
	wordlistOpenAPICmd.AddCommand(wordlistOpenAPISearchCmd)

	wordlistSeclistsCmd.AddCommand(wordlistSeclistsListCmd)
	wordlistSeclistsCmd.AddCommand(wordlistSeclistsFetchCmd)

	wordlistCmd.AddCommand(wordlistListCmd)
	wordlistCmd.AddCommand(wordlistUpdateCmd)
	wordlistCmd.AddCommand(wordlistCompileCmd)
	wordlistCmd.AddCommand(wordlistSeclistsCmd)
	wordlistCmd.AddCommand(wordlistOpenAPICmd)
}
