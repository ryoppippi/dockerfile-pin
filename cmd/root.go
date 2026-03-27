package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "dockerfile-pin",
	Short: "Pin Dockerfile and docker-compose images to digests",
	Long:  "A CLI tool that adds @sha256:<digest> to FROM lines in Dockerfiles and image fields in docker-compose.yml to prevent supply chain attacks.",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func Execute() error {
	return rootCmd.Execute()
}
