package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zcash/lightwalletd/common"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display lightwalletd version",
	Long:  `Display lightwalletd version.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("lightwalletd version: ", common.Version)
		fmt.Println("from commit: ", common.GitCommit)
		fmt.Println("on: ", common.BuildDate)
		fmt.Println("by: ", common.BuildUser)

	},
}
