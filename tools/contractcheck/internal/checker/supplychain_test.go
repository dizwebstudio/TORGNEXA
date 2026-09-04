package checker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testCheckoutCommit  = "1111111111111111111111111111111111111111"
	testDownloadCommit  = "2222222222222222222222222222222222222222"
	testSetupGoCommit   = "3333333333333333333333333333333333333333"
	testSetupNodeCommit = "6666666666666666666666666666666666666666"
	testUploadCommit    = "4444444444444444444444444444444444444444"
	testAttestCommit    = "5555555555555555555555555555555555555555"
	testDigestA         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDigestB         = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testDigestC         = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testDigestD         = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func TestSupplyChainPolicyAcceptsMinimalRepository(t *testing.T) {
	t.Parallel()
	root := writeValidSupplyChainRepository(t)
	if err := CheckSupplyChain(context.Background(), root); err != nil {
		t.Fatalf("valid supply-chain repository rejected: %v", err)
	}
}

func TestSupplyChainPolicyAcceptsPublishCheckoutWithContentsWrite(t *testing.T) {
	t.Parallel()
	root := writeValidSupplyChainRepository(t)
	replaceFixture(t, root, ".github/workflows/release.yml", "    steps:\n      - run: make publish", "    steps:\n      - uses: actions/checkout@"+testCheckoutCommit+"\n        with:\n          persist-credentials: false\n      - run: make publish")
	if err := CheckSupplyChain(context.Background(), root); err != nil {
		t.Fatalf("protected publish checkout with contents:write rejected: %v", err)
	}
}

func TestSupplyChainPolicyAcceptsExactReviewedRiskException(t *testing.T) {
	t.Parallel()
	root := writeValidSupplyChainRepository(t)
	writeRiskExceptionFixture(t, root, "example.invalid/module@v1.2.3", time.Now().UTC().Add(24*time.Hour))
	if err := CheckSupplyChain(context.Background(), root); err != nil {
		t.Fatalf("valid exact risk exception rejected: %v", err)
	}
}

func TestSupplyChainPolicyRejectsUnsafeConfigurations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*testing.T, string)
		wantErr string
	}{
		{
			name: "mutable action ref",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "actions/checkout@"+testCheckoutCommit, "actions/checkout@v7")
			},
			wantErr: "40-character lowercase commit",
		},
		{
			name: "action allowlist drift",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", testCheckoutCommit, "9999999999999999999999999999999999999999")
			},
			wantErr: "commit does not match",
		},
		{
			name: "unknown action with full commit",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "actions/setup-go@"+testSetupGoCommit, "example/unknown@"+testSetupGoCommit)
			},
			wantErr: "is not registered",
		},
		{
			name: "Docker action without digest",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "      - name: Run repository checks\n        run: ./scripts/check.sh", "      - uses: docker://postgres:18\n      - name: Run repository checks\n        run: ./scripts/check.sh")
			},
			wantErr: "docker action",
		},
		{
			name: "uppercase action commit",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", testCheckoutCommit, strings.Repeat("A", 40))
			},
			wantErr: "40-character lowercase commit",
		},
		{
			name: "unknown pinned image",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "docker-compose.yml", "postgres:18-alpine@sha256:"+testDigestC, "unknown/example:1.0.0@sha256:"+testDigestC)
			},
			wantErr: "is not approved",
		},
		{
			name: "image without digest",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "docker-compose.yml", "postgres:18-alpine@sha256:"+testDigestC, "postgres:18")
			},
			wantErr: "include exactly one sha256 digest",
		},
		{
			name: "dynamic Dockerfile base",
			mutate: func(t *testing.T, root string) {
				mustWriteFixture(t, root, "Dockerfile", "FROM ${BASE}\n")
			},
			wantErr: "reference must be static",
		},
		{
			name: "tag only Dockerfile base",
			mutate: func(t *testing.T, root string) {
				mustWriteFixture(t, root, "Dockerfile", "FROM postgres:18\n")
			},
			wantErr: "include exactly one sha256 digest",
		},
		{
			name: "broad workflow permissions",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "permissions:\n  contents: read", "permissions: write-all")
			},
			wantErr: "top-level permissions must be exactly contents: read",
		},
		{
			name: "pull request target",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "  pull_request:", "  pull_request_target:")
			},
			wantErr: "pull_request_target is forbidden",
		},
		{
			name: "pull request trigger filters protected paths",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "  pull_request:\n", "  pull_request:\n    paths:\n      - docs/**\n")
			},
			wantErr: "triggers must be exactly unfiltered",
		},
		{
			name: "pull request id token",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "      contents: read\n    env:", "      contents: read\n      id-token: write\n    env:")
			},
			wantErr: "pull-request job \"go\" may not request id-token: write",
		},
		{
			name: "other pull request workflow uses deployment environment",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/security.yml", "  source:\n    runs-on:", "  source:\n    environment: privileged\n    runs-on:")
			},
			wantErr: "pull-request job \"source\" may not use a deployment environment",
		},
		{
			name: "other pull request workflow receives job secrets",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/security.yml", "  source:\n    runs-on:", "  source:\n    secrets: inherit\n    runs-on:")
			},
			wantErr: "pull-request job \"source\" may not receive job-level secrets",
		},
		{
			name: "other pull request workflow references GitHub token",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/security.yml", "      - run: make security", "      - env:\n          TOKEN: ${{ github['token'] }}\n        run: make security")
			},
			wantErr: "pull-request job \"source\" may not reference secrets or the GitHub token",
		},
		{
			name: "expression interpolated into shell",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "run: ./scripts/check.sh", "run: echo ${{ github.event.pull_request.title }}")
			},
			wantErr: "expressions must enter run steps",
		},
		{
			name: "mutable runner label",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "runs-on: ubuntu-24.04", "runs-on: ubuntu-latest")
			},
			wantErr: "runs-on must be exactly ubuntu-24.04",
		},
		{
			name: "checkout credentials persisted",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "persist-credentials: false", "persist-credentials: true")
			},
			wantErr: "persist-credentials: false",
		},
		{
			name: "setup go cache enabled",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "cache: false", "cache: true")
			},
			wantErr: "setup-go must set cache: false",
		},
		{
			name: "architecture diff uses shallow history",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "fetch-depth: 0", "fetch-depth: 1")
			},
			wantErr: "full history",
		},
		{
			name: "architecture checkout does not clean workspace",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "clean: true", "clean: false")
			},
			wantErr: "must cleanly select",
		},
		{
			name: "architecture diff uses merge checkout",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "${{ github.event.pull_request.head.sha || github.sha }}", "${{ github.sha }}")
			},
			wantErr: "exact pull-request head",
		},
		{
			name: "architecture diff has wrong base binding",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "${{ github.event.pull_request.base.sha }}", "${{ github.sha }}")
			},
			wantErr: "must bind exact base/head",
		},
		{
			name: "HEAD repository code runs before trusted verifier",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "      - name: Build the trusted base verifier and validate the pull-request diff", "      - name: Run untrusted HEAD code prematurely\n        run: ./scripts/check.sh\n      - name: Build the trusted base verifier and validate the pull-request diff")
			},
			wantErr: "trusted architecture step 3",
		},
		{
			name: "trusted verifier executes HEAD gate script",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", `"$trusted_checker" --root "$GITHUB_WORKSPACE" --base "$BASE_REVISION" --head "$HEAD_REVISION"`, `./scripts/check-architecture.sh --base "$BASE_REVISION" --head "$HEAD_REVISION"`)
			},
			wantErr: "canonical base-revision binary",
		},
		{
			name: "trusted verifier builds from head revision",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", `worktree add --detach "$trusted_base" "$BASE_REVISION"`, `worktree add --detach "$trusted_base" "$HEAD_REVISION"`)
			},
			wantErr: "canonical base-revision binary",
		},
		{
			name: "trusted verifier enables dependency network",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "GOPROXY=off", "GOPROXY=https://proxy.example.invalid")
			},
			wantErr: "canonical base-revision binary",
		},
		{
			name: "trusted verifier accepts job condition",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "  go:\n    runs-on:", "  go:\n    if: github.event_name == 'pull_request'\n    runs-on:")
			},
			wantErr: "may not define job-level if",
		},
		{
			name: "trusted verifier accepts deployment environment",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "  go:\n    runs-on:", "  go:\n    environment: privileged\n    runs-on:")
			},
			wantErr: "may not define job-level environment",
		},
		{
			name: "trusted verifier accepts inherited secrets",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "  go:\n    runs-on:", "  go:\n    secrets: inherit\n    runs-on:")
			},
			wantErr: "may not define job-level secrets",
		},
		{
			name: "trusted job references secret expression",
			mutate: func(t *testing.T, root string) {
				appendFixture(t, root, ".github/workflows/ci.yml", "      - name: Leak secret\n        env:\n          VALUE: ${{   secrets.PRIVATE_VALUE }}\n        run: ./post-check.sh\n")
			},
			wantErr: "may not reference secrets or the GitHub token",
		},
		{
			name: "trusted verifier accepts shell startup injection",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "      GOWORK: \"off\"", "      GOWORK: \"off\"\n      BASH_ENV: ./untrusted-startup.sh")
			},
			wantErr: "env must contain only the approved Go safety settings",
		},
		{
			name: "trusted verifier accepts custom shell",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "        run: |\n          set -euo pipefail", "        shell: ./untrusted-shell\n        run: |\n          set -euo pipefail")
			},
			wantErr: "step 3 has unapproved fields",
		},
		{
			name: "ci adds bypass job",
			mutate: func(t *testing.T, root string) {
				appendFixture(t, root, ".github/workflows/ci.yml", "  bypass:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 5\n    permissions:\n      contents: read\n    steps:\n      - run: ./untrusted.sh\n")
			},
			wantErr: "exactly the go and javascript jobs",
		},
		{
			name: "release omits architecture gate",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/release.yml", "      - run: ./scripts/check-architecture.sh\n", "")
			},
			wantErr: "must run the static architecture gate",
		},
		{
			name: "release bypasses security",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/release.yml", "needs: [sbom, security]", "needs: [sbom]")
			},
			wantErr: "must depend on \"security\"",
		},
		{
			name: "release gate always runs",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/release.yml", "publish:\n    needs: verify", "publish:\n    if: always()\n    needs: verify")
			},
			wantErr: "release gate job \"publish\" may not use always()",
		},
		{
			name: "attestation job missing OIDC",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/release.yml", "      id-token: write", "      id-token: none")
			},
			wantErr: "release job \"attest\" permissions must be exactly",
		},
		{
			name: "publication job has package write",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/release.yml", "      contents: write\n    steps:\n      - run: make publish", "      contents: write\n      packages: write\n    steps:\n      - run: make publish")
			},
			wantErr: "job \"publish\" may not request packages: write",
		},
		{
			name: "continue on error",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/release.yml", "security:\n    needs: build", "security:\n    continue-on-error: true\n    needs: build")
			},
			wantErr: "may not continue on error",
		},
		{
			name: "module replacement",
			mutate: func(t *testing.T, root string) {
				appendFixture(t, root, "tools/contractcheck/go.mod", "\nreplace example.invalid/module => ../local\n")
			},
			wantErr: "replace directives are forbidden",
		},
		{
			name: "unregistered module",
			mutate: func(t *testing.T, root string) {
				mustMkdirAll(t, filepath.Join(root, "tools", "extra"))
				mustWriteFile(t, filepath.Join(root, "tools", "extra", "go.mod"), []byte("module github.com/torgnexa/torgnexa/tools/extra\n\ngo 1.26.0\n\ntoolchain go1.26.7\n"))
			},
			wantErr: "module inventory must be exactly",
		},
		{
			name: "unsupported package ecosystem",
			mutate: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, "package.json"), []byte(`{"name":"bypass"}`))
			},
			wantErr: "package ecosystem is not registered",
		},
		{
			name: "tool capability missing",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, toolVersionsPath, `,{"name":"trivy","version":"v0.70.0","source":"https://github.com/aquasecurity/trivy/releases/download/v0.70.0/trivy.tar.gz","archive_sha256":"`+testDigestC+`","binary_sha256":"`+testDigestD+`"}`, "")
			},
			wantErr: "archive_tools must contain exactly",
		},
		{
			name: "tool module checksum drift",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, toolVersionsPath, "h1:ZsSdiDb0AtTpLFVol5z91gbMei9ZiLEPG/pZjZujp7c=", "h1:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=")
			},
			wantErr: "module checksum must match",
		},
		{
			name: "binary omitted from release inventory",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, releaseInventoryPath, `,{"name":"worker","package":"./cmd/worker","platforms":["linux/amd64"]}`, "")
			},
			wantErr: "binary inventory must exactly match",
		},
		{
			name: "runtime platform unsupported",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, releaseInventoryPath, `["linux/amd64","linux/arm64"]`, `["linux/wasm64"]`)
			},
			wantErr: "platform \"linux/wasm64\" is not allowed",
		},
		{
			name: "public release without license",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, releaseInventoryPath, `"public_release_ready":false`, `"public_release_ready":true`)
			},
			wantErr: "requires a real top-level LICENSE",
		},
		{
			name: "unapproved exception",
			mutate: func(t *testing.T, root string) {
				mustWriteFixture(t, root, "supply-chain/risk-exceptions.json", `{"version":1,"approval_enforced":false,"exceptions":[{"id":"risk-1","category":"vulnerability","severity":"high","target":"module","finding_id":"CVE-1","justification":"temporary","compensating_controls":["not reachable"],"owner":"security","approved_by":"release","expires_at":"2099-01-01T00:00:00Z"}]}`)
			},
			wantErr: "exceptions require approval_enforced: true",
		},
		{
			name: "secret finding cannot be excepted",
			mutate: func(t *testing.T, root string) {
				mustWriteFixture(t, root, "supply-chain/risk-exceptions.json", `{"version":1,"approval_enforced":false,"exceptions":[{"id":"risk-1","category":"secret","severity":"critical","target":"repository","scope":"repository@v1.2.3","release":"v1.2.3","finding_id":"synthetic-secret","justification":"This must never be accepted.","compensating_controls":["none"],"ticket":"SEC-1","owner":"security-owner","approved_by":"release-approver","approved_at":"2026-08-01T00:00:00Z","expires_at":"2099-01-01T00:00:00Z"}]}`)
			},
			wantErr: "category is invalid",
		},
		{
			name: "wildcard risk scope",
			mutate: func(t *testing.T, root string) {
				writeRiskExceptionFixture(t, root, "example.invalid/module@*", time.Now().UTC().Add(24*time.Hour))
			},
			wantErr: "scope must bind target",
		},
		{
			name: "expired risk exception",
			mutate: func(t *testing.T, root string) {
				writeRiskExceptionFixture(t, root, "example.invalid/module@v1.2.3", time.Now().UTC().Add(-time.Hour))
			},
			wantErr: "must have a future RFC3339 expiry",
		},
		{
			name: "license categories overlap",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "supply-chain/license-policy.json", `"denied_spdx":["AGPL-3.0-only"]`, `"denied_spdx":["AGPL-3.0-only","MIT"]`)
			},
			wantErr: "appears in both",
		},
		{
			name: "unknown licenses allowed",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, "supply-chain/license-policy.json", `"unknown_license_policy":"deny"`, `"unknown_license_policy":"allow"`)
			},
			wantErr: "unknown_license_policy must be deny",
		},
		{
			name: "local action path escape",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "      - name: Run repository checks\n        run: ./scripts/check.sh", "      - uses: ./../outside\n      - name: Run repository checks\n        run: ./scripts/check.sh")
			},
			wantErr: "path traversal is forbidden",
		},
		{
			name: "missing local action",
			mutate: func(t *testing.T, root string) {
				replaceFixture(t, root, ".github/workflows/ci.yml", "      - name: Run repository checks\n        run: ./scripts/check.sh", "      - uses: ./.github/actions/missing\n      - name: Run repository checks\n        run: ./scripts/check.sh")
			},
			wantErr: "does not exist",
		},
		{
			name: "local action symlink",
			mutate: func(t *testing.T, root string) {
				mustMkdirAll(t, filepath.Join(root, ".github", "actions", "real"))
				mustWriteFile(t, filepath.Join(root, ".github", "actions", "real", "action.yml"), []byte("name: real\nruns: {using: composite, steps: []}\n"))
				if err := os.Symlink("real", filepath.Join(root, ".github", "actions", "linked")); err != nil {
					t.Fatalf("create symlink: %v", err)
				}
				replaceFixture(t, root, ".github/workflows/ci.yml", "      - name: Run repository checks\n        run: ./scripts/check.sh", "      - uses: ./.github/actions/linked\n      - name: Run repository checks\n        run: ./scripts/check.sh")
			},
			wantErr: "may not be a symlink",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := writeValidSupplyChainRepository(t)
			test.mutate(t, root)
			assertErrorContains(t, CheckSupplyChain(context.Background(), root), test.wantErr)
		})
	}
}

func TestSupplyChainPolicyValidatesInputsAndCancellation(t *testing.T) {
	t.Parallel()
	assertErrorContains(t, CheckSupplyChain(nil, "."), "context is required")
	assertErrorContains(t, CheckSupplyChain(context.Background(), ""), "repository root is required")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertErrorContains(t, CheckSupplyChain(ctx, writeValidSupplyChainRepository(t)), "validation interrupted")
}

func TestPinnedVersionPolicy(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v1.2.3", "v0.0.0-20260709232956-b9395ee17fa0", "v2.3.4-rc.1+incompatible"} {
		if !validPinnedVersion(version) {
			t.Errorf("pinned version %q was rejected", version)
		}
	}
	for _, version := range []string{"latest", "main", "v1", "v1.2", "v1.2.3@main", "v1.2.3-01", "v1.2.3-a..b", "v1.2.3+build..1"} {
		if validPinnedVersion(version) {
			t.Errorf("mutable or malformed version %q was accepted", version)
		}
	}
}

func writeValidSupplyChainRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, command := range []string{"api", "mcp", "scheduler", "worker"} {
		mustMkdirAll(t, filepath.Join(root, "cmd", command))
		mustWriteFile(t, filepath.Join(root, "cmd", command, "main.go"), []byte("package main\nfunc main() {}\n"))
	}
	mustWriteFixture(t, root, "go.mod", "module github.com/torgnexa/torgnexa\n\ngo 1.26.0\n\ntoolchain go1.26.7\n")
	mustWriteFixture(t, root, "sdk/go/go.mod", "module github.com/torgnexa/torgnexa-sdk-go\n\ngo 1.23.0\n")
	mustWriteFixture(t, root, "sdk/examples/go/go.mod", "module github.com/torgnexa/torgnexa-sdk-examples-go\n\ngo 1.23.0\n\nrequire github.com/torgnexa/torgnexa-sdk-go v0.0.0\n\nreplace github.com/torgnexa/torgnexa-sdk-go => ../../go\n")
	mustWriteFixture(t, root, "tools/contractcheck/go.mod", "module github.com/torgnexa/torgnexa/tools/contractcheck\n\ngo 1.26.0\n\ntoolchain go1.26.7\n")
	mustWriteFixture(t, root, "tools/sdkgen/go.mod", "module github.com/torgnexa/torgnexa/tools/sdkgen\n\ngo 1.23.0\n")
	mustWriteFixture(t, root, "tools/securitytools/go.mod", `module github.com/torgnexa/torgnexa/tools/securitytools

go 1.26.0

toolchain go1.26.7

tool (
	github.com/securego/gosec/v2/cmd/gosec
	golang.org/x/vuln/cmd/govulncheck
)

require (
	github.com/securego/gosec/v2 v2.28.0
	golang.org/x/vuln v1.6.0
)
`)
	mustWriteFixture(t, root, "tools/securitytools/go.sum", `github.com/securego/gosec/v2 v2.28.0 h1:ZsSdiDb0AtTpLFVol5z91gbMei9ZiLEPG/pZjZujp7c=
github.com/securego/gosec/v2 v2.28.0/go.mod h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
golang.org/x/vuln v1.6.0 h1:FeMO9Rm/HwyduOztbvKcOw+zvDEPr4I4aQNSfevFcKY=
golang.org/x/vuln v1.6.0/go.mod h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
`)
	for _, relative := range []string{
		"frontend/package-lock.json",
		"frontend/package.json",
		"integrations/n8n-nodes-torgnexa/package-lock.json",
		"integrations/n8n-nodes-torgnexa/package.json",
		"sdk/python/pyproject.toml",
		"sdk/typescript/package.json",
	} {
		mustWriteFixture(t, root, relative, "{}\n")
	}
	mustWriteFixture(t, root, "docker-compose.yml", validComposeFixture())
	mustWriteFixture(t, root, actionPinsPath, validActionPinsFixture())
	mustWriteFixture(t, root, toolVersionsPath, validToolVersionsFixture())
	mustWriteFixture(t, root, releaseInventoryPath, validReleaseInventoryFixture())
	mustWriteFixture(t, root, "supply-chain/license-policy.json", `{"version":1,"allowed_spdx":["Apache-2.0","MIT"],"review_required_spdx":["MPL-2.0"],"denied_spdx":["AGPL-3.0-only"],"selected_or_choices":[],"unknown_license_policy":"deny"}`)
	mustWriteFixture(t, root, "supply-chain/risk-exceptions.json", `{"version":1,"approval_enforced":false,"exceptions":[]}`)
	mustWriteFixture(t, root, ".github/workflows/ci.yml", validCIWorkflowFixture())
	mustWriteFixture(t, root, ".github/workflows/release.yml", validReleaseWorkflowFixture())
	mustWriteFixture(t, root, ".github/workflows/security.yml", validSecurityWorkflowFixture())
	return root
}

func writeRiskExceptionFixture(t *testing.T, root, scope string, expiresAt time.Time) {
	t.Helper()
	approvedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	manifest := fmt.Sprintf(`{"version":1,"approval_enforced":true,"exceptions":[{"id":"risk-1","category":"vulnerability","severity":"high","target":"example.invalid/module","scope":%q,"release":"v1.2.3","finding_id":"GO-2026-0001","justification":"Synthetic bounded exception for policy testing.","compensating_controls":["Affected call path is disabled by a mandatory policy gate."],"ticket":"SEC-123","owner":"security-owner","approved_by":"release-approver","approved_at":%q,"expires_at":%q}]}`, scope, approvedAt, expiresAt.UTC().Format(time.RFC3339))
	mustWriteFixture(t, root, "supply-chain/risk-exceptions.json", manifest)
	mustWriteFixture(t, root, ".github/CODEOWNERS", "/supply-chain/risk-exceptions.json @torgnexa/security-reviewers\n")
}

func validComposeFixture() string {
	return "services:\n" +
		"  clickhouse:\n    image: clickhouse/clickhouse-server:26.6@sha256:" + testDigestA + "\n" +
		"  kafka:\n    image: apache/kafka:4.3.1@sha256:" + testDigestB + "\n" +
		"  postgres:\n    image: postgres:18-alpine@sha256:" + testDigestC + "\n" +
		"  valkey:\n    image: valkey/valkey:9.1.1-alpine@sha256:" + testDigestD + "\n"
}

func validActionPinsFixture() string {
	return `{"version":1,"actions":[` +
		`{"name":"actions/attest","version":"v4.2.2","commit":"` + testAttestCommit + `"},` +
		`{"name":"actions/checkout","version":"v7.0.1","commit":"` + testCheckoutCommit + `"},` +
		`{"name":"actions/download-artifact","version":"v8.0.1","commit":"` + testDownloadCommit + `"},` +
		`{"name":"actions/setup-go","version":"v7.0.0","commit":"` + testSetupGoCommit + `"},` +
		`{"name":"actions/setup-node","version":"v7.0.0","commit":"` + testSetupNodeCommit + `"},` +
		`{"name":"actions/upload-artifact","version":"v7.0.1","commit":"` + testUploadCommit + `"}]}`
}

func validToolVersionsFixture() string {
	return `{"version":1,"go_tools":[` +
		`{"name":"gosec","module":"github.com/securego/gosec/v2/cmd/gosec","version":"v2.28.0","sum":"h1:ZsSdiDb0AtTpLFVol5z91gbMei9ZiLEPG/pZjZujp7c="},` +
		`{"name":"govulncheck","module":"golang.org/x/vuln/cmd/govulncheck","version":"v1.6.0","sum":"h1:FeMO9Rm/HwyduOztbvKcOw+zvDEPr4I4aQNSfevFcKY="}],` +
		`"archive_tools":[` +
		`{"name":"syft","version":"v1.50.0","source":"https://github.com/anchore/syft/releases/download/v1.50.0/syft.tar.gz","archive_sha256":"` + testDigestA + `","binary_sha256":"` + testDigestB + `"},` +
		`{"name":"trivy","version":"v0.70.0","source":"https://github.com/aquasecurity/trivy/releases/download/v0.70.0/trivy.tar.gz","archive_sha256":"` + testDigestC + `","binary_sha256":"` + testDigestD + `"}],` +
		`"binary_tools":[{"name":"cosign","version":"v3.1.3","source":"https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-amd64","binary_sha256":"` + testDigestA + `"}]}`
}

func validReleaseInventoryFixture() string {
	return `{"version":1,"public_release_ready":false,"binaries":[` +
		`{"name":"api","package":"./cmd/api","platforms":["linux/amd64"]},` +
		`{"name":"mcp","package":"./cmd/mcp","platforms":["linux/amd64"]},` +
		`{"name":"scheduler","package":"./cmd/scheduler","platforms":["linux/amd64"]},` +
		`{"name":"worker","package":"./cmd/worker","platforms":["linux/amd64"]}],` +
		`"development_runtime":[` +
		`{"name":"clickhouse","image":"clickhouse/clickhouse-server:26.6@sha256:` + testDigestA + `","platforms":["linux/amd64","linux/arm64"]},` +
		`{"name":"kafka","image":"apache/kafka:4.3.1@sha256:` + testDigestB + `","platforms":["linux/amd64","linux/arm64"]},` +
		`{"name":"postgres","image":"postgres:18-alpine@sha256:` + testDigestC + `","platforms":["linux/amd64","linux/arm64"]},` +
		`{"name":"valkey","image":"valkey/valkey:9.1.1-alpine@sha256:` + testDigestD + `","platforms":["linux/amd64","linux/arm64"]}]}`
}

func validCIWorkflowFixture() string {
	return `name: ci
on:
  push:
  pull_request:
permissions:
  contents: read
jobs:
  go:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions:
      contents: read
    env:
      GOTELEMETRY: "off"
      GOTOOLCHAIN: local
      GOWORK: "off"
    steps:
      - name: Check out the exact revision without persisted credentials
        uses: actions/checkout@` + testCheckoutCommit + `
        with:
          clean: true
          fetch-depth: 0
          persist-credentials: false
          ref: ${{ github.event.pull_request.head.sha || github.sha }}
      - name: Install the approved Go toolchain
        uses: actions/setup-go@` + testSetupGoCommit + `
        with:
          go-version: 1.26.7
          cache: false
      - name: Build the trusted base verifier and validate the pull-request diff
        if: github.event_name == 'pull_request'
        env:
          BASE_REVISION: ${{ github.event.pull_request.base.sha }}
          HEAD_REVISION: ${{ github.event.pull_request.head.sha }}
        run: |
          ` + strings.ReplaceAll(trustedArchitectureRun, "\n", "\n          ") + `
      - name: Run repository checks
        run: ./scripts/check.sh
  javascript:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions:
      contents: read
    steps:
      - name: Check out the exact revision without persisted credentials
        uses: actions/checkout@` + testCheckoutCommit + `
        with:
          clean: true
          fetch-depth: 0
          persist-credentials: false
          ref: ${{ github.event.pull_request.head.sha || github.sha }}
      - name: Install the approved Node toolchain
        uses: actions/setup-node@` + testSetupNodeCommit + `
        with:
          node-version: 22.16.0
      - name: Verify JavaScript dependency policy
        run: ./scripts/check-js-supply-chain.sh repository
      - name: Install the exact n8n build graph without lifecycle scripts
        working-directory: integrations/n8n-nodes-torgnexa
        run: npm ci --ignore-scripts --no-audit --no-fund
      - name: Build, test, and inspect the n8n package
        working-directory: integrations/n8n-nodes-torgnexa
        run: |
          npm run build
          npm run test:offline
          npm run verify:package
          npm pack --dry-run --ignore-scripts
`
}

func validReleaseWorkflowFixture() string {
	return `name: release
on: [push]
permissions:
  contents: read
jobs:
  tests:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@` + testCheckoutCommit + `
        with:
          persist-credentials: false
      - run: make test
      - run: ./scripts/check-architecture.sh
  contracts:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions: {}
    steps:
      - run: make contracts
  policy:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions: {}
    steps:
      - run: make policy
  build:
    needs: [tests, contracts, policy]
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    permissions: {}
    steps:
      - uses: actions/upload-artifact@` + testUploadCommit + `
  sbom:
    needs: build
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    permissions: {}
    steps:
      - uses: actions/download-artifact@` + testDownloadCommit + `
  security:
    needs: build
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    permissions:
      security-events: write
    steps:
      - run: make security
  attest:
    needs: [sbom, security]
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    environment: release-signing
    permissions:
      artifact-metadata: write
      attestations: write
      contents: read
      id-token: write
    steps:
      - uses: actions/attest@` + testAttestCommit + `
  verify:
    needs: attest
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions: {}
    steps:
      - run: make verify
  publish:
    needs: verify
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    environment: release-publication
    permissions:
      contents: write
    steps:
      - run: make publish
`
}

func validSecurityWorkflowFixture() string {
	return `name: security
on:
  pull_request:
permissions:
  contents: read
jobs:
  source:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions:
      contents: read
    steps:
      - name: Install the approved Node toolchain
        uses: actions/setup-node@` + testSetupNodeCommit + `
        with:
          node-version: 22.16.0
      - run: make security
`
}

func mustWriteFixture(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte(contents))
}

func replaceFixture(t *testing.T, root, relative, old, replacement string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	// #nosec G304 -- root is a test-owned TempDir and relative is a fixed synthetic fixture path.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", relative, err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("fixture %s does not contain %q", relative, old)
	}
	mustWriteFile(t, path, []byte(strings.Replace(string(data), old, replacement, 1)))
}

func appendFixture(t *testing.T, root, relative, addition string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	// #nosec G304,G703 -- root is created by t.TempDir and relative is a fixed synthetic fixture path.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open fixture %s: %v", relative, err)
	}
	if _, err := file.WriteString(addition); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			t.Fatalf("append fixture %s: %v; close fixture: %v", relative, err, closeErr)
		}
		t.Fatalf("append fixture %s: %v", relative, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture %s: %v", relative, err)
	}
}

func TestTask093CommunityImageRepositoriesAreExplicitlyApproved(t *testing.T) {
	refs := []string{
		"golang:1.26.7-alpine3.23@sha256:" + testDigestA,
		"node:22.16.0-alpine3.21@sha256:" + testDigestD,
		"dxflrs/garage:v2.3.0@sha256:" + testDigestB,
		"quay.io/keycloak/keycloak:26.7.0@sha256:" + testDigestC,
	}
	for _, ref := range refs {
		if err := validateImmutableImage(ref); err != nil {
			t.Fatalf("validateImmutableImage(%q): %v", ref, err)
		}
	}
	if err := validateImmutableImage("example.invalid/unreviewed/runtime:v1@sha256:" + testDigestD); err == nil {
		t.Fatal("unreviewed runtime repository must remain denied")
	}
}
