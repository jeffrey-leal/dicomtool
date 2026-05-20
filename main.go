package main

import (
	"os"

	"github.com/jeffr/dicomtool/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
