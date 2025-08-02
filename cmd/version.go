package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Print the version information for pgcopy`,
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("pgcopy version %s\n", version)
		fmt.Printf("Git commit: %s\n", commit)
		fmt.Printf("Built on: %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
