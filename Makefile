SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
export GOTOOLCHAIN := local
export GOWORK := off

.PHONY: fmt fmt-check test vet contracts sdk-generate sdk-check frontend-check js-policy architecture migrations migration-baseline migration-rebaseline migrations-runtime backup-restore-runtime upgrade-runtime policy sandbox conformance performance production-qualification p3-qualification p4-qualification p4-publish p4-policy community-check community-init community-up community-demo-user community-e2e community-down community-status package-index package-index-check check build
fmt:
	find . -type f -name '*.go' -not -path './vendor/*' -print0 | xargs -0 -r gofmt -w
fmt-check:
	@files="$$(find . -type f -name '*.go' -not -path './vendor/*' -print0 | xargs -0 -r gofmt -l)"; \
	if [[ -n "$$files" ]]; then echo "Go files require formatting:"; echo "$$files"; exit 1; fi
test:
	go test ./...
vet:
	go vet ./...
contracts:
	./scripts/check-contracts.sh
sdk-generate:
	go -C tools/sdkgen run . --root ../..
sdk-check:
	./scripts/check-generated-sdks.sh
frontend-check:
	./scripts/check-frontend-shell.sh
js-policy:
	./scripts/check-js-supply-chain.sh repository
architecture:
	./scripts/check-architecture.sh
migrations:
	./scripts/check-migrations.sh

migration-baseline:
	./scripts/check-pre-v1-baseline.sh

migration-rebaseline:
	@echo "WARNING: this rewrites verified pre-v1 migration_history after archiving all 74 rows"
	TORGNEXA_ALLOW_PRE_V1_REBASELINE=I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY docker compose run --rm --entrypoint /deploy/postgres/rebaseline-pre-v1.sh migrate
migrations-runtime:
	./scripts/check-tenancy-postgres.sh
backup-restore-runtime:
	./scripts/check-postgres-backup-restore.sh
upgrade-runtime:
	./scripts/check-postgres-upgrade.sh
policy:
	./scripts/check-supply-chain.sh
sandbox:
	./scripts/check-connector-sandbox-linux.sh
conformance:
	./scripts/check-connector-conformance.sh
performance:
	./scripts/check-performance-slo.sh
production-qualification:
	./scripts/check-production-qualification.sh
p3-qualification:
	./scripts/check-p3-release-qualification.sh
p4-qualification:
	./scripts/check-p4-go-live.sh
p4-publish:
	./scripts/promote-github-release.sh
p4-policy:
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/p4_qualification_test.py
	PYTHONDONTWRITEBYTECODE=1 python3 -c 'import ast,pathlib; [ast.parse(pathlib.Path(p).read_text()) for p in ["scripts/p4_common.py","scripts/p4_hosting_rules.py","scripts/p4_live_connectors.py","scripts/p4_posture.py","scripts/p4_release_stage.py","scripts/p4_root_evidence.py"]]'
	@for script in scripts/check-p4-go-live.sh scripts/package-release-evidence.sh scripts/stage-github-release.sh scripts/promote-github-release.sh scripts/verify-release-evidence-external.sh; do bash -n $$script; done
community-check:
	./scripts/check-community-deployment.sh
community-init:
	./scripts/init-community-env.sh
community-up: community-check
	@if [[ ! -f .env ]]; then ./scripts/init-community-env.sh; fi
	TORGNEXA_WORKER_UPLOADS_ENABLED=$${TORGNEXA_WORKER_UPLOADS_ENABLED:-true} TORGNEXA_CLAMAV_ADDRESS=$${TORGNEXA_CLAMAV_ADDRESS:-clamav:3310} docker compose --env-file .env up -d --build
	./scripts/ensure-community-demo-user.sh
community-demo-user:
	./scripts/ensure-community-demo-user.sh
community-e2e: community-up
	./scripts/community-e2e.sh
community-down:
	docker compose --env-file .env down
community-status:
	docker compose --env-file .env ps
package-index:
	python3 scripts/generate-package-index.py
package-index-check:
	python3 scripts/generate-package-index.py --check
check: fmt-check test vet contracts architecture migrations policy sandbox conformance performance sdk-check frontend-check js-policy p4-policy community-check package-index-check
build:
	go build -trimpath -buildvcs=false ./cmd/...
