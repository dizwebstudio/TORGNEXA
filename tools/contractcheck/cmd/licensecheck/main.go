package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/torgnexa/torgnexa/tools/contractcheck/internal/licensepolicy"
)

func main() {
	policy := flag.String("policy", "", "license policy JSON path")
	report := flag.String("report", "", "sanitized Trivy license report path")
	flag.Parse()
	if flag.NArg() != 0 || *policy == "" || *report == "" {
		fmt.Fprintln(os.Stderr, "licensecheck: -policy and -report are required; positional arguments are forbidden")
		os.Exit(2)
	}
	result, err := licensepolicy.CheckFiles(*policy, *report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "licensecheck: %v\n", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "licensecheck: encode result: %v\n", err)
		os.Exit(1)
	}
}
