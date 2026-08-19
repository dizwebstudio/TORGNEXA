package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/torgnexa/torgnexa/tools/contractcheck/internal/releasecheck"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("releasecheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "release evidence directory")
	manifest := flags.String("manifest", "", "manifest path relative to the evidence directory")
	modeValue := flags.String("mode", "", "validation mode: dry-run or public")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "releasecheck: positional arguments are not supported")
		return 2
	}
	if *root == "" || *manifest == "" || *modeValue == "" {
		fmt.Fprintln(stderr, "releasecheck: -root, -manifest, and -mode are required")
		return 2
	}
	mode, err := releasecheck.ParseMode(*modeValue)
	if err != nil {
		fmt.Fprintf(stderr, "releasecheck: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := releasecheck.Validate(ctx, releasecheck.Options{
		Root:         *root,
		ManifestPath: *manifest,
		Mode:         mode,
	})
	if err != nil {
		fmt.Fprintf(stderr, "releasecheck: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "releasecheck: encode result: %v\n", err)
		return 1
	}
	return 0
}
