package main

import (
	"fmt"
	"os"

	"zeuf/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "zeuf:", err)
		os.Exit(1)
	}
}
