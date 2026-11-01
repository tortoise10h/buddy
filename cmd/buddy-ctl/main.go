package main

import (
	"os"

	"github.com/tortoise10h/buddy/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
