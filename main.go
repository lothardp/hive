package main

import (
	"os"

	"github.com/lothardp/hive/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
