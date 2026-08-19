package architecture

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseNameStatusCoversAllSafeChangeKinds(t *testing.T) {
	t.Parallel()
	data := []byte("A\x00added.go\x00M\x00modified.go\x00D\x00deleted.go\x00R100\x00old.go\x00new.go\x00C075\x00source.go\x00copy.go\x00")
	changes, err := parseNameStatus(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 5 || changes[3].OldPath != "old.go" || changes[3].Path != "new.go" || changes[4].Status != 'C' {
		t.Fatalf("parseNameStatus() = %#v", changes)
	}
}

func TestParseNameStatusRejectsUnsafeOrIncompleteInput(t *testing.T) {
	t.Parallel()
	tests := [][]byte{
		[]byte("U\x00conflict.go\x00"),
		[]byte("A\x00../escape.go\x00"),
		[]byte("A\x00missing-terminator.go"),
		[]byte("R100\x00old-only.go\x00"),
	}
	for _, data := range tests {
		if _, err := parseNameStatus(data); err == nil {
			t.Fatalf("parseNameStatus(%q) unexpectedly passed", data)
		}
	}
}

func TestCheckDiffRequiresNewExactReviewForCoreChange(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\ntype Model struct{ Version int }\n")
	commitFixture(t, root, "core without review")
	head := runGit(t, root, "rev-parse", "HEAD")
	_, err := CheckDiff(context.Background(), root, base, head)
	if err == nil || !strings.Contains(err.Error(), "requires a new architecture review") {
		t.Fatalf("CheckDiff() error = %v", err)
	}
}

func TestCheckDiffAcceptsNewExactReviewForCoreChange(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\ntype Model struct{ Version int }\n")
	writeTestFile(t, root, "tasks/issues/081-synthetic.md", "# Task 081\n")
	writeTestJSON(t, root, reviewsDir+"/081-core-model.json", validTestReview("081", "internal/core/synthetic/model.go"))
	commitFixture(t, root, "core with review")
	head := runGit(t, root, "rev-parse", "HEAD")
	report, err := CheckDiff(context.Background(), root, base, head)
	if err != nil {
		t.Fatalf("CheckDiff() error = %v", err)
	}
	if report.Changes != 3 || report.Reviews != 2 {
		t.Fatalf("CheckDiff() report = %#v", report)
	}
}

func TestCheckDiffAcceptsFreshSupplementalStageReview(t *testing.T) {
	root := writeArchitectureFixture(t)
	writeTestFile(t, root, "tasks/issues/089-fx.md", "# Task 089\n")
	writeTestJSON(t, root, reviewsDir+"/089-fx-foundation.json", validTestReview("089", "internal/core/synthetic/model.go"))
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\ntype Model struct{ Version int }\n")
	record := validTestReview("089", "internal/core/synthetic/model.go")
	record.Stage = "b"
	record.ID = "ARCH-089B"
	writeTestJSON(t, root, reviewsDir+"/089b-fx-storage.json", record)
	commitFixture(t, root, "supplemental stage review")
	head := runGit(t, root, "rev-parse", "HEAD")
	if _, err := CheckDiff(context.Background(), root, base, head); err != nil {
		t.Fatalf("CheckDiff() error = %v", err)
	}
}

func TestCheckDiffRejectsAcceptedADRMutationAndRevisionAmbiguity(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "adr/0001-synthetic.md", "# ADR 0001 changed\n\nStatus: Accepted\n")
	commitFixture(t, root, "rewrite accepted ADR")
	head := runGit(t, root, "rev-parse", "HEAD")
	if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "accepted ADRs are immutable") {
		t.Fatalf("ADR mutation error = %v", err)
	}
	if _, err := CheckDiff(context.Background(), root, "main", head); err == nil || !strings.Contains(err.Error(), "full lowercase 40-hex") {
		t.Fatalf("ambiguous revision error = %v", err)
	}
}

func TestCheckDiffAcceptsReviewedNewDomain(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	configuration := loadTestPolicy(t, root)
	configuration.Modules = append(configuration.Modules, module{Path: "internal/platform/syntheticport", Kind: "platform_capability"})
	writeTestJSON(t, root, policyPath, configuration)
	writeTestFile(t, root, "internal/platform/syntheticport/port.go", "package syntheticport\ntype Port interface{}\n")
	writeTestFile(t, root, "tasks/issues/081-synthetic.md", "# Task 081\n")
	record := validTestReview("081", "architecture/policy.json")
	record.ChangeClass = "new_domain"
	record.GapAudit.Decision = "extend_existing_capability"
	record.Scopes = []string{"architecture/policy.json", "internal/platform/syntheticport/port.go"}
	writeTestJSON(t, root, reviewsDir+"/081-new-domain.json", record)
	commitFixture(t, root, "reviewed new domain")
	head := runGit(t, root, "rev-parse", "HEAD")
	if _, err := CheckDiff(context.Background(), root, base, head); err != nil {
		t.Fatalf("reviewed new-domain CheckDiff() error = %v", err)
	}
}

func TestCheckDiffProtectsGateAndAppendOnlyReviews(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{
			name: "gate self change",
			mutate: func(t *testing.T, root string) {
				data := readTestFile(t, root, "scripts/check-architecture.sh")
				writeTestFile(t, root, "scripts/check-architecture.sh", data+"# reviewed synthetic change\n")
				// #nosec G302 -- the synthetic gate script must remain executable after mutation.
				if err := os.Chmod(filepath.Join(root, "scripts", "check-architecture.sh"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			want: "pillar_change or mixed review",
		},
		{
			name: "review mutation",
			mutate: func(t *testing.T, root string) {
				replaceTestFile(t, root, reviewsDir+"/080-synthetic.json", "Synthetic implementation inside", "Updated implementation inside")
			},
			want: "append-only",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeArchitectureFixture(t)
			initializeGitFixture(t, root)
			base := runGit(t, root, "rev-parse", "HEAD")
			test.mutate(t, root)
			commitFixture(t, root, test.name)
			head := runGit(t, root, "rev-parse", "HEAD")
			if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckDiff() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCheckDiffBindsSensitiveScopeToRequiredReviewClass(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	data := readTestFile(t, root, "scripts/check-architecture.sh")
	writeTestFile(t, root, "scripts/check-architecture.sh", data+"# synthetic self-gate mutation\n")
	// #nosec G302 -- the synthetic gate script must remain executable after mutation.
	if err := os.Chmod(filepath.Join(root, "scripts", "check-architecture.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "tasks/issues/081-synthetic.md", "# Task 081\n")
	writeTestJSON(t, root, reviewsDir+"/081-wrong-class.json", validTestReview("081", "scripts/check-architecture.sh"))
	writeTestFile(t, root, "tasks/issues/082-synthetic.md", "# Task 082\n")
	pillar := validTestReview("082", "docs/54-architecture-freeze-v1.md")
	pillar.ChangeClass = "pillar_change"
	pillar.GapAudit.Decision = "architecture_change"
	pillar.FrozenPillars = []string{canonicalPillars[0]}
	reference := "adr/0036-synthetic-gate.md"
	pillar.ADR = &reference
	writeTestJSON(t, root, reviewsDir+"/082-unrelated-pillar.json", pillar)
	writeTestFile(t, root, reference, validDecisionADRFixture())
	commitFixture(t, root, "split review bypass")
	head := runGit(t, root, "rev-parse", "HEAD")
	if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "exact pillar_change or mixed review coverage") {
		t.Fatalf("split class/scope bypass error = %v", err)
	}
}

func TestCheckDiffFailsOnDirtyOrUntrackedHead(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	revision := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "untracked.txt", "synthetic")
	if _, err := CheckDiff(context.Background(), root, revision, revision); err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked error = %v", err)
	}
}

func TestCheckDiffRejectsIgnoredUntrackedAndSparseCheckout(t *testing.T) {
	t.Run("ignored untracked", func(t *testing.T) {
		root := writeArchitectureFixture(t)
		writeTestFile(t, root, ".gitignore", "ignored-provider.go\n")
		initializeGitFixture(t, root)
		revision := runGit(t, root, "rev-parse", "HEAD")
		writeTestFile(t, root, "ignored-provider.go", "package hidden\n")
		if _, err := CheckDiff(context.Background(), root, revision, revision); err == nil || !strings.Contains(err.Error(), "untracked") {
			t.Fatalf("ignored untracked error = %v", err)
		}
	})
	t.Run("skip worktree", func(t *testing.T) {
		root := writeArchitectureFixture(t)
		initializeGitFixture(t, root)
		revision := runGit(t, root, "rev-parse", "HEAD")
		runGit(t, root, "update-index", "--skip-worktree", "internal/core/synthetic/model.go")
		if _, err := CheckDiff(context.Background(), root, revision, revision); err == nil || !strings.Contains(err.Error(), "skip-worktree") {
			t.Fatalf("sparse checkout error = %v", err)
		}
	})
}

func TestCheckDiffFailsClosedWhenMergeBasePolicyIsMissing(t *testing.T) {
	root := writeArchitectureFixture(t)
	policyData := readTestFile(t, root, policyPath)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(policyPath))); err != nil {
		t.Fatal(err)
	}
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, policyPath, policyData)
	commitFixture(t, root, "introduce architecture policy")
	head := runGit(t, root, "rev-parse", "HEAD")
	if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "cannot read merge-base architecture policy") {
		t.Fatalf("missing base policy error = %v", err)
	}
}

func TestCheckDiffRejectsStaleDivergedHead(t *testing.T) {
	root := writeArchitectureFixture(t)
	initializeGitFixture(t, root)
	runGit(t, root, "checkout", "-b", "feature")
	writeTestFile(t, root, "notes/feature.md", "synthetic feature branch\n")
	commitFixture(t, root, "feature commit")
	head := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "checkout", "main")
	writeTestFile(t, root, "notes/base.md", "trusted base advanced\n")
	commitFixture(t, root, "advance base")
	base := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "checkout", "feature")
	if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "stale or diverged") {
		t.Fatalf("diverged head error = %v", err)
	}
}

func TestReviewCoverageAndProviderPolicyChangesAreBound(t *testing.T) {
	t.Parallel()
	providerEvidence := &providerReview{ID: "alpha"}
	reviews := []review{
		{ChangeClass: "implementation", Scopes: []string{"scripts/check-architecture.sh"}},
		{ChangeClass: "pillar_change", Scopes: []string{"docs/54-architecture-freeze-v1.md"}},
		{ChangeClass: "new_provider", Scopes: []string{"connectors/beta/client.go"}, Provider: providerEvidence},
	}
	if reviewCoversPathByClass(reviews, "scripts/check-architecture.sh", "pillar_change") {
		t.Fatal("unrelated pillar review satisfied implementation scope")
	}
	if reviewCoversProviderPath(reviews, "connectors/beta/client.go", "beta", "new_provider") {
		t.Fatal("provider alpha evidence satisfied provider beta path")
	}
	splitMixed := []review{
		{ChangeClass: "mixed", Scopes: []string{"connectors/alpha/client.go"}, Provider: &providerReview{ID: "alpha"}},
		{ChangeClass: "mixed", Scopes: []string{"internal/core/synthetic/model.go"}, Provider: &providerReview{ID: "beta"}},
	}
	if oneMixedReviewCovers(splitMixed, "alpha", []string{"connectors/alpha/client.go", "internal/core/synthetic/model.go"}) {
		t.Fatal("two unrelated mixed records satisfied the single-record union requirement")
	}
	before := []provider{{ID: "alpha", AllowedExternalImports: []string{}}}
	after := []provider{{ID: "alpha", AllowedExternalImports: []string{"example.invalid/sdk"}}}
	_, changed, _ := providerDefinitionChanges(before, after)
	if len(changed) != 1 || changed[0] != "alpha" {
		t.Fatalf("providerDefinitionChanges() changed = %#v", changed)
	}
	beforePolicy := policy{Providers: before}
	afterPolicy := policy{Providers: after}
	if !policyControlsChanged(beforePolicy, afterPolicy) {
		t.Fatal("provider allowlist widening was not classified as a policy control change")
	}
}

func TestCheckDiffEnforcesRetiredProviderLifecycle(t *testing.T) {
	t.Run("reviewed retirement", func(t *testing.T) {
		root := writeAdmittedProviderFixture(t)
		initializeGitFixture(t, root)
		base := runGit(t, root, "rev-parse", "HEAD")
		retireAdmittedProvider(t, root, "082", "082")
		commitFixture(t, root, "retire provider with explicit tombstone")
		head := runGit(t, root, "rev-parse", "HEAD")
		if _, err := CheckDiff(context.Background(), root, base, head); err != nil {
			t.Fatalf("reviewed provider retirement rejected: %v", err)
		}
	})

	t.Run("retirement task must own fresh review", func(t *testing.T) {
		root := writeAdmittedProviderFixture(t)
		initializeGitFixture(t, root)
		base := runGit(t, root, "rev-parse", "HEAD")
		retireAdmittedProvider(t, root, "083", "082")
		commitFixture(t, root, "retire provider with unrelated review task")
		head := runGit(t, root, "rev-parse", "HEAD")
		if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "one fresh pillar_change or mixed review for Task 083") {
			t.Fatalf("mismatched retirement review error = %v", err)
		}
	})

	t.Run("forged tombstone was never active", func(t *testing.T) {
		root := writeArchitectureFixture(t)
		initializeGitFixture(t, root)
		base := runGit(t, root, "rev-parse", "HEAD")
		writeRetiredProviderFixture(t, root, "ghost", "081")
		commitFixture(t, root, "forge retired provider history")
		head := runGit(t, root, "rev-parse", "HEAD")
		if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "was not an active provider in the merge base") {
			t.Fatalf("forged tombstone error = %v", err)
		}
	})

	t.Run("existing tombstones are immutable", func(t *testing.T) {
		before := []retiredProvider{{ID: "synthetic", RetirementTask: "082"}}
		after := []retiredProvider{{ID: "synthetic", RetirementTask: "083"}}
		_, changed, removed := retiredProviderDefinitionChanges(before, after)
		if len(changed) != 1 || changed[0] != "synthetic" || len(removed) != 0 {
			t.Fatalf("retiredProviderDefinitionChanges() changed=%v removed=%v", changed, removed)
		}
	})
}

func TestAdmissionPrerequisiteMustBeCompletedInMergeBase(t *testing.T) {
	root := writeArchitectureFixture(t)
	writeTestFile(t, root, "tasks/issues/010-synthetic.md", "# Task 010\n\n## Status\n\nIn progress.\n")
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "tasks/issues/010-synthetic.md", "# Task 010\n\n## Status\n\nCompleted on 2026-08-09.\n")
	commitFixture(t, root, "claim prerequisite completion")
	if r, err := openRepository(root); err != nil {
		t.Fatal(err)
	} else if r.baseTaskCompleted(context.Background(), base, "010") {
		t.Fatal("head-only task completion was trusted as merge-base evidence")
	}
}

func TestCheckDiffBindsAdmissionPrerequisiteCompletionToFreshTaskReview(t *testing.T) {
	tests := []struct {
		name       string
		addReview  func(*testing.T, string)
		wantError  string
		shouldPass bool
	}{
		{
			name: "matching task and exact issue scope",
			addReview: func(t *testing.T, root string) {
				writeTestJSON(t, root, reviewsDir+"/010-completion.json", validTestReview("010", "tasks/issues/010-synthetic.md"))
			},
			shouldPass: true,
		},
		{
			name:      "completion without review",
			wantError: "requires one fresh task-bound architecture review",
		},
		{
			name: "review belongs to another task",
			addReview: func(t *testing.T, root string) {
				writeTestFile(t, root, "tasks/issues/081-synthetic.md", "# Task 081\n")
				writeTestJSON(t, root, reviewsDir+"/081-wrong-task.json", validTestReview("081", "tasks/issues/010-synthetic.md"))
			},
			wantError: "requires one fresh task-bound architecture review",
		},
		{
			name: "task review omits issue scope",
			addReview: func(t *testing.T, root string) {
				writeTestFile(t, root, "notes/completion.md", "Synthetic completion evidence.\n")
				writeTestJSON(t, root, reviewsDir+"/010-wrong-scope.json", validTestReview("010", "notes/completion.md"))
			},
			wantError: "requires one fresh task-bound architecture review",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeArchitectureFixture(t)
			writeAdmissionTaskFixtures(t, root, "010")
			initializeGitFixture(t, root)
			base := runGit(t, root, "rev-parse", "HEAD")
			writeTestFile(t, root, "tasks/issues/010-synthetic.md", "# Task 010\n\n## Status\n\nCompleted on 2026-08-09.\n")
			if test.addReview != nil {
				test.addReview(t, root)
			}
			commitFixture(t, root, test.name)
			head := runGit(t, root, "rev-parse", "HEAD")
			_, err := CheckDiff(context.Background(), root, base, head)
			if test.shouldPass {
				if err != nil {
					t.Fatalf("task-bound completion rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("CheckDiff() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestCheckDiffMakesCompletedAdmissionPrerequisiteIssueImmutable(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{
			name: "modify",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "tasks/issues/010-synthetic.md", "# Task 010\n\n## Status\n\nCompleted on 2026-08-09.\n\nRewritten evidence.\n")
			},
			want: "issue is immutable",
		},
		{
			name: "delete",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "tasks/issues/010-synthetic.md")); err != nil {
					t.Fatal(err)
				}
			},
			want: "was removed or became ambiguous",
		},
		{
			name: "rename",
			mutate: func(t *testing.T, root string) {
				if err := os.Rename(filepath.Join(root, "tasks/issues/010-synthetic.md"), filepath.Join(root, "tasks/issues/010-renamed.md")); err != nil {
					t.Fatal(err)
				}
			},
			want: "may not be renamed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeArchitectureFixture(t)
			writeAdmissionTaskFixtures(t, root, "")
			initializeGitFixture(t, root)
			base := runGit(t, root, "rev-parse", "HEAD")
			test.mutate(t, root)
			commitFixture(t, root, test.name+" completed prerequisite")
			head := runGit(t, root, "rev-parse", "HEAD")
			if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckDiff() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCheckDiffRejectsSameChangeCompletionAndAdmissionOpening(t *testing.T) {
	root := writeArchitectureFixture(t)
	writeAdmissionTaskFixtures(t, root, "010")
	initializeGitFixture(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	writeTestFile(t, root, "tasks/issues/010-synthetic.md", "# Task 010\n\n## Status\n\nCompleted on 2026-08-09.\n")
	configuration := loadTestPolicy(t, root)
	configuration.ProviderAdmission.Enabled = true
	configuration.ProviderCompositionModule = canonicalProviderCompositionModule
	configuration.Modules = append(configuration.Modules, module{Path: canonicalProviderCompositionModule, Kind: "infrastructure_adapter"})
	sort.Slice(configuration.Modules, func(i, j int) bool { return configuration.Modules[i].Path < configuration.Modules[j].Path })
	writeTestJSON(t, root, policyPath, configuration)
	writeTestFile(t, root, canonicalProviderCompositionModule+"/runtime.go", "package builtinruntime\n")
	record := validTestReview("010", policyPath)
	record.ChangeClass = "pillar_change"
	record.GapAudit.Decision = "architecture_change"
	record.FrozenPillars = []string{canonicalPillars[2]}
	record.Scopes = []string{policyPath, "tasks/issues/010-synthetic.md"}
	adrPath := "adr/0036-synthetic-admission.md"
	record.ADR = &adrPath
	writeTestFile(t, root, adrPath, validDecisionADRFixture())
	writeTestJSON(t, root, reviewsDir+"/010-admission.json", record)
	commitFixture(t, root, "complete prerequisite and open admission together")
	head := runGit(t, root, "rev-parse", "HEAD")
	if _, err := CheckDiff(context.Background(), root, base, head); err == nil || !strings.Contains(err.Error(), "completed in the merge base") {
		t.Fatalf("same-change admission error = %v", err)
	}
}

func writeAdmissionTaskFixtures(t *testing.T, root, incomplete string) {
	t.Helper()
	for _, task := range []string{"010", "025", "029", "064"} {
		status := "Completed on 2026-08-09."
		if task == incomplete {
			status = "In progress."
		}
		writeTestFile(t, root, "tasks/issues/"+task+"-synthetic.md", "# Task "+task+"\n\n## Status\n\n"+status+"\n")
	}
}

func retireAdmittedProvider(t *testing.T, root, retirementTask, reviewTask string) {
	t.Helper()
	configuration := loadTestPolicy(t, root)
	if len(configuration.Providers) != 1 {
		t.Fatalf("provider fixture inventory = %#v", configuration.Providers)
	}
	current := configuration.Providers[0]
	configuration.Providers = []provider{}
	configuration.RetiredProviders = []retiredProvider{{
		ID: current.ID, Implementation: current.Implementation, Manifest: current.Manifest,
		ConnectorSpec: current.ConnectorSpec, CapabilityAudit: current.CapabilityAudit,
		ConformancePlan: current.ConformancePlan, RetirementTask: retirementTask,
	}}
	writeTestJSON(t, root, policyPath, configuration)
	if err := os.Remove(filepath.Join(root, "connectors/synthetic/connector.go")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "tasks/issues/"+retirementTask+"-synthetic.md", "# Task "+retirementTask+"\n")
	if reviewTask != retirementTask {
		writeTestFile(t, root, "tasks/issues/"+reviewTask+"-synthetic.md", "# Task "+reviewTask+"\n")
	}
	record := validTestReview(reviewTask, policyPath)
	record.ChangeClass = "pillar_change"
	record.GapAudit.Decision = "architecture_change"
	record.FrozenPillars = []string{canonicalPillars[2]}
	record.Scopes = []string{policyPath, "connectors/synthetic/connector.go"}
	adrPath := "adr/0036-synthetic-retirement.md"
	record.ADR = &adrPath
	writeTestFile(t, root, adrPath, validDecisionADRFixture())
	writeTestJSON(t, root, reviewsDir+"/"+reviewTask+"-synthetic-retirement.json", record)
}

func TestBoundedGitBufferPreservesLimit(t *testing.T) {
	t.Parallel()
	buffer := &boundedBuffer{maximum: 4}
	written, err := buffer.Write([]byte("abcdef"))
	if err != nil || written != 6 || !buffer.exceeded || buffer.String() != "abcd" {
		t.Fatalf("boundedBuffer = (%d, %v, %t, %q)", written, err, buffer.exceeded, buffer.String())
	}
}

func validDecisionADRFixture() string {
	return `# ADR 0036: Synthetic gate decision

Status: Accepted

## Context

Synthetic context explains why the architecture gate decision is required.

## Decision

Synthetic decision changes a governed architecture control with explicit review.

## Consequences

Synthetic consequences describe compatibility and implementation obligations.

## Alternatives considered

Synthetic alternatives were evaluated and rejected with documented rationale.

## Compatibility impact

Synthetic compatibility analysis confirms the bounded contract implications.

## Migration and data impact

Synthetic migration analysis confirms no hidden persisted-data transformation.

## Security and privacy impact

Synthetic security analysis covers confidentiality, integrity, and privacy risk.

## Operational impact

Synthetic operations analysis covers rollout, rollback, monitoring, and ownership.
`
}

func initializeGitFixture(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init", "--initial-branch=main")
	commitFixture(t, root, "base")
}

func commitFixture(t *testing.T, root, message string) {
	t.Helper()
	runGit(t, root, "add", "--all")
	runGit(t, root, "-c", "user.name=TORGNEXA Test", "-c", "user.email=test@example.invalid", "commit", "-m", message)
}
