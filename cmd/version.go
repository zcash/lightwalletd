package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zcash/lightwalletd/common"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Dispaly lightwalletd version",
	Long:  `Dispaly lightwalletd version.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("lightwalletd version", common.Version)
	},
}
