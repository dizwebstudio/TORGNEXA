package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/migration"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "migrationcheck: positional arguments are not supported")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalog, err := migration.LoadCatalog(ctx, *root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrationcheck: %v\n", err)
		os.Exit(1)
	}
	latest := catalog.Migrations[len(catalog.Migrations)-1].Version
	fmt.Printf("Migration catalog validation passed: %d migrations, latest=%06d\n", len(catalog.Migrations), latest)
}
