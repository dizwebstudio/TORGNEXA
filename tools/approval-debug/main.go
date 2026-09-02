package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/approvalrepo"
)

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	org, err := tenancy.ParseOrganizationID("0198b8d0-0000-7000-8000-000000000001")
	if err != nil {
		panic(err)
	}
	ws, err := tenancy.ParseWorkspaceID("0198b8d0-0000-7000-8000-000000000002")
	if err != nil {
		panic(err)
	}
	scope, err := tenancy.NewScope(org, ws)
	if err != nil {
		panic(err)
	}
	repo, err := approvalrepo.New(db)
	if err != nil {
		panic(err)
	}
	items, err := repo.ListPolicies(context.Background(), scope, 100)
	fmt.Printf("items=%#v err=%v\n", items, err)
}
