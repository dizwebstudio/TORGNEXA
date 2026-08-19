// Command architecturecheck enforces the TORGNEXA architecture freeze.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/architecture"
)

func main() {
	root := flag.String("root", ".", "repository root")
	base := flag.String("base", "", "pull request base revision (full 40-hex SHA)")
	head := flag.String("head", "", "pull request head revision (full 40-hex SHA)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "architecturecheck: positional arguments are not supported")
		os.Exit(2)
	}
	if (*base == "") != (*head == "") {
		fmt.Fprintln(os.Stderr, "architecturecheck: --base and --head must be provided together")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var (
		report architecture.Report
		err    error
	)
	if *base == "" {
		report, err = architecture.CheckRepository(ctx, *root)
	} else {
		report, err = architecture.CheckDiff(ctx, *root, *base, *head)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "architecturecheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(
		"Architecture validation passed: modules=%d providers=%d reviews=%d changes=%d\n",
		report.Modules,
		report.Providers,
		report.Reviews,
		report.Changes,
	)
}
