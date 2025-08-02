// Package main provides the entry point for the pgcopy application
package main

import (
	"fmt"
	"os"

	"github.com/koltyakov/pgcopy/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
