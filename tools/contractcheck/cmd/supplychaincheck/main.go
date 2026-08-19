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
		fmt.Fprintln(os.Stderr, "supplychaincheck: positional arguments are not supported")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := checker.CheckSupplyChain(ctx, *root); err != nil {
		fmt.Fprintf(os.Stderr, "supplychaincheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Supply-chain policy validation passed")
}
