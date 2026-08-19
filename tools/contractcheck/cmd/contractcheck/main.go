package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/torgnexa/torgnexa/tools/contractcheck/internal/checker"
)

func main() {
	root := flag.String("root", "../..", "repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "contractcheck: positional arguments are not supported")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := checker.Check(ctx, *root); err != nil {
		fmt.Fprintf(os.Stderr, "contractcheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Contract validation passed")
}
