package architecture

import (
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var providerIdentifierTokens = []string{
	"aliexpress", "autoru", "avito", "cian", "dzen", "egais", "instagram",
	"magnitmarket", "megamarket", "moysklad", "ozon", "rutube", "telegram",
	"threads", "wildberries", "yandexmarket", "youtube",
}

var providerLiteralTokens = map[string]struct{}{
	"1c": {}, "aliexpress": {}, "auto.ru": {}, "avito": {}, "cian": {},
	"dzen": {}, "egais": {}, "instagram": {}, "max": {}, "megamarket": {},
	"moysklad": {}, "ok": {}, "ozon": {}, "rutube": {}, "telegram": {},
	"threads": {}, "vk": {}, "wb": {}, "wildberries": {}, "yandex_market": {},
	"youtube": {},
}

var unambiguousProviderLiteralTokens = map[string]struct{}{
	"aliexpress": {}, "auto.ru": {}, "avito": {}, "cian": {}, "dzen": {},
	"egais": {}, "instagram": {}, "megamarket": {}, "moysklad": {},
	"ozon": {}, "rutube": {}, "telegram": {}, "threads": {},
	"wildberries": {}, "yandex_market": {}, "youtube": {},
}

var allowedToolGoRoots = []string{
	"tools/architecturecheck/",
	"tools/contractcheck/",
	"tools/migrationcheck/",
	"tools/sdkgen/",
}

func (r *repository) checkGoTree(ctx context.Context, configuration *policy, found *problems) int {
	discovered := make(map[string]struct{})
	files := make([]string, 0)
	r.walkGoFiles(ctx, &files, discovered, found)
	sort.Strings(files)

	configured := make(map[string]module, len(configuration.Modules))
	for _, item := range configuration.Modules {
		configured[item.Path] = item
		if _, ok := discovered[item.Path]; !ok {
			found.add(policyPath, "registered module %q has no Go package", item.Path)
		}
	}
	for item := range discovered {
		if _, ok := configured[item]; !ok {
			found.add(policyPath, "Go package %q is not registered", item)
		}
	}
	for _, item := range configuration.Modules {
		validateModuleKind(item, found)
	}

	for _, relative := range files {
		if err := ctx.Err(); err != nil {
			return len(discovered)
		}
		r.checkGoFile(ctx, relative, configuration, configured, found)
	}
	return len(discovered)
}

func (r *repository) walkGoFiles(ctx context.Context, files *[]string, packages map[string]struct{}, found *problems) {
	entries := 0
	err := filepath.WalkDir(r.root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			found.add(".", "walk Go inventory: %v", walkErr)
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relativeOS, err := filepath.Rel(r.root, current)
		if err != nil {
			found.add(".", "resolve walked Go path: %v", err)
			return nil
		}
		relative := filepath.ToSlash(relativeOS)
		// frontend/node_modules is package-manager material, not first-party Go
		// source. Ignore the entire configured dependency root before inspecting
		// its expected symlinks and nested vendor directories. Source-side
		// node_modules/vendor trees remain fail-closed everywhere else.
		if relative == "frontend/node_modules" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative != "." {
			entries++
			if entries > maxTreeEntries {
				found.add(".", "repository tree inventory exceeds %d entries", maxTreeEntries)
				return fs.SkipAll
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			found.add(relative, "symlinks are forbidden in the repository Go inventory")
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if relative == ".git" {
				return filepath.SkipDir
			}
			if entry.Name() == "vendor" {
				found.add(relative, "vendor directories are forbidden because their compiled Go content bypasses module checksum and architecture inventory guarantees")
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			found.add(relative, "stat Go file: %v", err)
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxGoFileBytes {
			found.add(relative, "Go file must be regular and at most %d bytes", maxGoFileBytes)
			return nil
		}
		if len(*files) >= maxGoFiles {
			found.add(".", "Go file inventory exceeds %d", maxGoFiles)
			return fs.SkipAll
		}
		*files = append(*files, relative)
		if !allowedGoSourcePath(relative) {
			found.add(relative, "Go source is outside the fail-closed first-party source-root inventory")
		}
		if strings.HasPrefix(relative, "internal/") {
			packages[pathDir(relative)] = struct{}{}
		}
		return nil
	})
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		found.add(".", "walk Go inventory failed: %v", err)
	}
}

func allowedGoSourcePath(relative string) bool {
	for _, prefix := range []string{
		"cmd/", "connectors/", "internal/app/", "internal/core/", "internal/platform/", "plugins/",
		"sdk/go/", "sdk/examples/go/",
	} {
		if strings.HasPrefix(relative, prefix) {
			return true
		}
	}
	for _, prefix := range allowedToolGoRoots {
		if strings.HasPrefix(relative, prefix) {
			return true
		}
	}
	return false
}

func pathDir(relative string) string {
	index := strings.LastIndexByte(relative, '/')
	if index < 0 {
		return "."
	}
	return relative[:index]
}

func validateModuleKind(item module, found *problems) {
	switch {
	case strings.HasPrefix(item.Path, "internal/app/") && item.Kind != "application":
		found.add(policyPath, "module %q must have kind application", item.Path)
	case strings.HasPrefix(item.Path, "internal/core/") && item.Kind != "core_domain" && item.Kind != "shared_types":
		found.add(policyPath, "module %q must have a Core kind", item.Path)
	case strings.HasPrefix(item.Path, "internal/platform/postgres/") && item.Kind != "infrastructure_adapter":
		found.add(policyPath, "module %q must have kind infrastructure_adapter", item.Path)
	case strings.HasPrefix(item.Path, "internal/platform/") && item.Kind == "application":
		found.add(policyPath, "platform module %q cannot have application kind", item.Path)
	}
}

func (r *repository) checkGoFile(ctx context.Context, relative string, configuration *policy, configured map[string]module, found *problems) {
	data, err := r.readRegular(relative, maxGoFileBytes)
	if err != nil {
		found.add(relative, "%v", err)
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, relative, data, parser.ParseComments)
	if err != nil {
		found.add(relative, "parse Go source: %v", err)
		return
	}
	sourceDir := pathDir(relative)
	source, registered := configured[sourceDir]
	if strings.HasPrefix(relative, "internal/") && !registered {
		found.add(relative, "source package is absent from architecture module inventory")
	}
	for _, imported := range parsed.Imports {
		value, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			found.add(relative, "invalid Go import literal")
			continue
		}
		if value == "C" {
			found.add(relative, "cgo is forbidden in first-party architecture-scanned Go source")
		}
		checkImportDirection(relative, source, registered, value, configuration, found)
	}
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(strings.TrimSpace(comment.Text), "//go:linkname") {
				found.add(relative, "//go:linkname is forbidden in first-party architecture-scanned Go source")
			}
		}
	}
	if isNonProviderRuntimePath(relative) && !isArchitecturePolicyFixtureSource(relative) && !isProviderCompositionSource(relative, configuration) {
		checkProviderSpecificCode(relative, parsed, set, configuration, found)
	}
}

func isArchitecturePolicyFixtureSource(relative string) bool {
	// This build-time policy package necessarily contains provider examples and
	// discriminator logic. Its complete directory is a self-protected TCB path
	// and PR diff validation executes the verifier built from the trusted base.
	return strings.HasPrefix(relative, "internal/platform/architecture/")
}

func isProviderCompositionSource(relative string, configuration *policy) bool {
	if configuration == nil || configuration.ProviderCompositionModule == "" {
		return false
	}
	return relative == configuration.ProviderCompositionModule || strings.HasPrefix(relative, configuration.ProviderCompositionModule+"/")
}

func isNonProviderRuntimePath(relative string) bool {
	return strings.HasPrefix(relative, "cmd/") || strings.HasPrefix(relative, "internal/app/") ||
		strings.HasPrefix(relative, "internal/core/") || strings.HasPrefix(relative, "internal/platform/")
}

func checkImportDirection(relative string, source module, registered bool, imported string, configuration *policy, found *problems) {
	root := configuration.ModulePath
	if strings.HasPrefix(relative, "internal/platform/pluginsecurity/") && pluginSecurityForbiddenImport(imported) {
		found.add(relative, "Task 025 plugin security boundary must remain non-executing and host-filesystem independent")
	}
	if imported == root || strings.HasPrefix(imported, root+"/cmd/") {
		found.add(relative, "importing an executable package is forbidden")
		return
	}
	providerImport := strings.HasPrefix(imported, root+"/connectors/") || strings.HasPrefix(imported, root+"/plugins/")
	firstPartyImport := imported == root || strings.HasPrefix(imported, root+"/")
	toolImport := strings.HasPrefix(imported, root+"/tools/")
	if registered {
		if source.Path == configuration.ProviderCompositionModule {
			if toolImport || strings.HasPrefix(imported, root+"/internal/app/") || (firstPartyImport && !strings.HasPrefix(imported, root+"/internal/") && !providerImport) {
				found.add(relative, "provider composition may depend only on internal host packages and registered provider implementations")
			}
			if providerImport && !registeredProviderImport(configuration, imported) {
				found.add(relative, "provider composition imports an unregistered provider implementation")
			}
			return
		}
		switch source.Kind {
		case "core_domain", "shared_types":
			if strings.HasPrefix(imported, root+"/") {
				if !strings.HasPrefix(imported, root+"/internal/core/") && !coreSharedImportAllowed(configuration, imported) {
					found.add(relative, "Core may import only standard/external packages or other Core packages")
				}
			} else if isExternalImport(imported) && !containsString(configuration.CoreExternalImports, imported) {
				found.add(relative, "Core external import is not explicitly approved by architecture policy")
			}
		case "platform_capability", "sdk_port", "infrastructure_adapter":
			if strings.HasPrefix(imported, root+"/internal/app/") || providerImport || toolImport ||
				(firstPartyImport && !strings.HasPrefix(imported, root+"/internal/")) {
				found.add(relative, "Platform may not depend on App or provider implementations")
			}
		case "application":
			if providerImport || toolImport || (firstPartyImport && !strings.HasPrefix(imported, root+"/internal/")) {
				found.add(relative, "App may not depend directly on provider implementations")
			}
		}
	}
	if strings.HasPrefix(relative, "cmd/") && (providerImport || toolImport ||
		(firstPartyImport && !strings.HasPrefix(imported, root+"/internal/"))) {
		found.add(relative, "executables may import only registered internal runtime packages")
	}
	if strings.HasPrefix(relative, "connectors/") || strings.HasPrefix(relative, "plugins/") {
		forbidden := providerForbiddenStandardLibrary(imported) || imported == "database/sql" ||
			strings.HasPrefix(imported, root+"/internal/platform/postgres/") ||
			strings.HasPrefix(imported, root+"/internal/core/") ||
			strings.HasPrefix(imported, root+"/internal/app/") || providerImport
		if strings.HasPrefix(imported, root+"/internal/") {
			allowed := false
			for _, prefix := range configuration.ProviderAllowedImports {
				full := root + "/" + prefix
				if imported == full || strings.HasPrefix(imported, full+"/") {
					allowed = true
					break
				}
			}
			forbidden = forbidden || !allowed
		}
		if isExternalImport(imported) && !strings.HasPrefix(imported, root+"/") {
			allowed := false
			for _, registered := range configuration.Providers {
				if relative == registered.Implementation || strings.HasPrefix(relative, registered.Implementation+"/") {
					allowed = containsString(registered.AllowedExternalImports, imported)
					break
				}
			}
			forbidden = forbidden || !allowed
		}
		if firstPartyImport && !strings.HasPrefix(imported, root+"/internal/") {
			forbidden = true
		}
		if forbidden {
			found.add(relative, "provider implementation bypasses the Connector SDK boundary")
		}
	}
}

func coreSharedImportAllowed(configuration *policy, imported string) bool {
	if configuration == nil {
		return false
	}
	root := configuration.ModulePath + "/"
	for _, allowed := range configuration.CoreSharedImports {
		full := root + allowed
		if imported == full || strings.HasPrefix(imported, full+"/") {
			return true
		}
	}
	return false
}

func registeredProviderImport(configuration *policy, imported string) bool {
	if configuration == nil {
		return false
	}
	root := configuration.ModulePath + "/"
	for _, item := range configuration.Providers {
		wanted := root + item.Implementation
		if imported == wanted || strings.HasPrefix(imported, wanted+"/") {
			return true
		}
	}
	return false
}

func pluginSecurityForbiddenImport(imported string) bool {
	if imported == "os" || imported == "io/fs" || imported == "path/filepath" || imported == "plugin" || imported == "syscall" || imported == "unsafe" {
		return true
	}
	return imported == "os/exec" || strings.HasPrefix(imported, "os/exec/") || imported == "runtime/cgo"
}

func providerForbiddenStandardLibrary(imported string) bool {
	if imported == "os" || imported == "io/fs" || imported == "path/filepath" || imported == "plugin" || imported == "syscall" || imported == "unsafe" {
		return true
	}
	for _, prefix := range []string{"net", "net/", "os/exec", "runtime", "runtime/"} {
		if imported == prefix || strings.HasPrefix(imported, prefix) {
			return true
		}
	}
	return false
}

func isExternalImport(imported string) bool {
	first := strings.SplitN(imported, "/", 2)[0]
	return strings.Contains(first, ".")
}

func containsString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func checkProviderSpecificCode(relative string, parsed *ast.File, set *token.FileSet, configuration *policy, found *problems) {
	providerCount := len(configuration.Providers) + len(configuration.RetiredProviders)
	literals := make(map[string]struct{}, len(unambiguousProviderLiteralTokens)+providerCount)
	registeredIdentifiers := make(map[string]struct{}, providerCount)
	for value := range unambiguousProviderLiteralTokens {
		literals[value] = struct{}{}
	}
	register := func(id string) {
		literals[strings.ToLower(id)] = struct{}{}
		registeredIdentifiers[strings.ToLower(strings.ReplaceAll(id, "-", ""))] = struct{}{}
	}
	for _, item := range configuration.Providers {
		register(item.ID)
	}
	for _, item := range configuration.RetiredProviders {
		register(item.ID)
	}
	contextName := "non-provider runtime"
	if strings.HasPrefix(relative, "internal/core/") {
		contextName = "Core"
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if identifier, ok := node.(*ast.Ident); ok {
			normalized := strings.ToLower(strings.ReplaceAll(identifier.Name, "_", ""))
			matched := false
			for _, token := range providerIdentifierTokens {
				if strings.Contains(normalized, token) {
					position := set.Position(identifier.Pos())
					found.add(relative, "provider-specific %s identifier is forbidden at line %d", contextName, position.Line)
					matched = true
					break
				}
			}
			if !matched {
				if _, forbidden := registeredIdentifiers[normalized]; forbidden {
					position := set.Position(identifier.Pos())
					found.add(relative, "registered provider identifier is forbidden in %s code at line %d", contextName, position.Line)
				}
			}
		}
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			value, err := strconv.Unquote(literal.Value)
			if err == nil {
				if _, forbidden := literals[strings.ToLower(value)]; forbidden {
					position := set.Position(literal.Pos())
					found.add(relative, "provider-specific %s string literal is forbidden at line %d", contextName, position.Line)
				}
			}
		}
		switch typed := node.(type) {
		case *ast.IfStmt:
			checkProviderCondition(relative, typed.Cond, set, found)
		case *ast.SwitchStmt:
			checkProviderSwitch(relative, typed, set, found)
		case *ast.BinaryExpr:
			checkProviderCondition(relative, typed, set, found)
		case *ast.IndexExpr:
			strict, channel, _, known := providerConditionFlags(typed.Index)
			if strict || (channel && known) {
				position := set.Position(typed.Pos())
				found.add(relative, "provider-specific table dispatch is forbidden at line %d", position.Line)
			}
		}
		return true
	})
}

func checkProviderCondition(relative string, expression ast.Node, set *token.FileSet, found *problems) {
	if expression == nil {
		return
	}
	strict, channel, _, known := providerConditionFlags(expression)
	if strict || (channel && known) {
		position := set.Position(expression.Pos())
		found.add(relative, "provider-specific Core branch or non-provider runtime branch is forbidden at line %d", position.Line)
	}
}

func checkProviderSwitch(relative string, statement *ast.SwitchStmt, set *token.FileSet, found *problems) {
	if statement.Tag == nil {
		return
	}
	strict, channel, _, _ := providerConditionFlags(statement.Tag)
	known := false
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			_, _, _, expressionKnown := providerConditionFlags(expression)
			known = known || expressionKnown
		}
	}
	if strict || (channel && known) {
		position := set.Position(statement.Pos())
		found.add(relative, "provider-specific Core branch or non-provider runtime branch is forbidden at line %d", position.Line)
	}
}

func providerConditionFlags(expression ast.Node) (strict, channel, nonEmpty, known bool) {
	hasStrictDiscriminator := false
	hasChannelDiscriminator := false
	hasNonEmptyLiteral := false
	hasKnownProviderLiteral := false
	ast.Inspect(expression, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.Ident:
			name := strings.ToLower(typed.Name)
			if strings.Contains(name, "provider") || strings.Contains(name, "platform") || strings.Contains(name, "connector") {
				hasStrictDiscriminator = true
			}
			if strings.Contains(name, "channel") {
				hasChannelDiscriminator = true
			}
		case *ast.BasicLit:
			if typed.Kind != token.STRING {
				break
			}
			value, err := strconv.Unquote(typed.Value)
			if err == nil && value != "" {
				hasNonEmptyLiteral = true
				if _, known := providerLiteralTokens[strings.ToLower(value)]; known {
					hasKnownProviderLiteral = true
				}
			}
		}
		return true
	})
	return hasStrictDiscriminator, hasChannelDiscriminator, hasNonEmptyLiteral, hasKnownProviderLiteral
}

func (r *repository) checkProviderInventory(ctx context.Context, configuration *policy, reviews map[string]review, found *problems) {
	discovered := make(map[string]string)
	for _, root := range canonicalProviderRoots {
		for _, item := range r.discoverProviderImplementations(ctx, root, found) {
			if previous, duplicate := discovered[item.ID]; duplicate {
				found.add(item.Path, "provider id is also implemented at %s", previous)
			}
			discovered[item.ID] = item.Path
		}
	}
	for _, item := range configuration.Providers {
		if implementation, ok := discovered[item.ID]; !ok || implementation != item.Implementation {
			found.add(policyPath, "provider %q implementation inventory does not match", item.ID)
		}
		for _, reference := range []string{item.Manifest, item.ConnectorSpec, item.CapabilityAudit, item.ConformancePlan} {
			if _, err := r.readRegular(reference, maxReviewBytes); err != nil {
				found.add(policyPath, "provider %q evidence %q: %v", item.ID, reference, err)
			}
		}
		if configuration.ProviderAdmission.Enabled && !r.providerUsesSDK(ctx, item.Implementation, configuration.ModulePath, found) {
			found.add(item.Implementation, "provider has no non-test Go implementation importing the Connector SDK")
		}
		reviewed := false
		for _, record := range reviews {
			if record.Provider != nil && providerReviewMatches(*record.Provider, item) {
				reviewed = true
				break
			}
		}
		if !reviewed {
			found.add(policyPath, "provider %q has no current architecture review matching its registered evidence", item.ID)
		}
	}
}

func providerReviewMatches(record providerReview, registered provider) bool {
	return record.ID == registered.ID && record.Manifest == registered.Manifest &&
		record.ConnectorSpec == registered.ConnectorSpec && record.CapabilityAudit == registered.CapabilityAudit &&
		record.ConformancePlan == registered.ConformancePlan
}

func (r *repository) providerUsesSDK(ctx context.Context, implementation, modulePath string, found *problems) bool {
	absolute := filepath.Join(r.root, filepath.FromSlash(implementation))
	wanted := modulePath + "/internal/platform/connectors"
	foundImport := false
	count := 0
	err := filepath.WalkDir(absolute, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is forbidden")
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || entry.Name() == "vendor" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		buildContext := build.Default
		buildContext.GOOS = "linux"
		buildContext.GOARCH = "amd64"
		buildContext.CgoEnabled = false
		matched, err := buildContext.MatchFile(filepath.Dir(current), entry.Name())
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
		count++
		if count > maxGoFiles {
			return fmt.Errorf("Go file count exceeds %d", maxGoFiles)
		}
		relativeOS, err := filepath.Rel(r.root, current)
		if err != nil {
			return err
		}
		relative := filepath.ToSlash(relativeOS)
		data, err := r.readRegular(relative, maxGoFileBytes)
		if err != nil {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), relative, data, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err == nil && value == wanted && (imported.Name == nil || (imported.Name.Name != "_" && imported.Name.Name != ".")) {
				foundImport = true
			}
		}
		return nil
	})
	if err != nil {
		found.add(implementation, "inspect Connector SDK route: %v", err)
	}
	return foundImport
}
