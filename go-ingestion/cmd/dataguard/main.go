package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "dataguard",
		Short: "DataGuard Rail — realtime data quality & lineage",
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "ingest",
			Short: "Ingest data from CSV or PostgreSQL and run quality checks",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println("ingest — not yet implemented")
				return nil
			},
		},
		&cobra.Command{
			Use:   "serve",
			Short: "Start REST API server",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println("serve — not yet implemented")
				return nil
			},
		},
	)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
