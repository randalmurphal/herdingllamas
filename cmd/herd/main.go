package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "herd",
		Short: "Multi-model debate orchestrator",
		Long:  "HerdingLlamas orchestrates interactive debates between LLM agents.",
	}

	root.AddCommand(debateCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
