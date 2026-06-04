package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version, BuildDate, Commit, and BuiltBy are set at build time via -ldflags.
var Version = "dev"
var BuildDate = "unknown"
var Commit = "unknown"
var BuiltBy = "source"

var versionVerbose bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the KiteString version",
	Long: `Print version information for this KiteString build.

Use --verbose to print full build metadata including Go version, build date,
commit hash, and platform — useful for bug reports and reproducibility.

Examples:
  ks version
  ks version --verbose`,
	Run: func(cmd *cobra.Command, args []string) {
		if versionVerbose {
			fmt.Printf("kitestring %s\n", Version)
			fmt.Printf("go:         %s\n", runtime.Version())
			fmt.Printf("build date: %s\n", BuildDate)
			fmt.Printf("commit:     %s\n", Commit)
			fmt.Printf("built by:   %s\n", BuiltBy)
			fmt.Printf("platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
		} else {
			fmt.Printf("kitestring %s\n", Version)
		}
	},
}

func init() {
	versionCmd.Flags().BoolVarP(&versionVerbose, "verbose", "V", false,
		"print full build metadata (go version, build date, commit, platform)")
}
