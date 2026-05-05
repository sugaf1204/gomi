package main

import (
	"fmt"
	"os"

	"github.com/sugaf1204/gomi/bootenv/internal/runner"
)

func main() {
	r := runner.Runner{}
	if err := r.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}
