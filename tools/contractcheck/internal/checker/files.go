package checker

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxContractSize       = 4 << 20
	maxContractFiles      = 10_000
	maxContractTotalBytes = 64 << 20
)

type contractFile struct {
	Rel  string
	Abs  string
	Data []byte
}

type inventory struct {
	contractsRoot string
	jsonFiles     []contractFile
	yamlFiles     []contractFile
	schemaFiles   []contractFile
	openAPIFiles  []contractFile
	protoFiles    []contractFile
}

func scanRepository(ctx context.Context, root string, problems *diagnostics) inventory {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		problems.add("", "resolve repository root: %v", err)
		return inventory{}
	}
	contractsRoot := filepath.Join(absRoot, "contracts")
	info, err := os.Lstat(contractsRoot)
	if err != nil {
		problems.add("contracts", "inspect directory: %v", err)
		return inventory{contractsRoot: contractsRoot}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		problems.add("contracts", "must be a real directory, not a symlink")
		return inventory{contractsRoot: contractsRoot}
	}

	result := inventory{contractsRoot: contractsRoot}
	fileCount := 0
	var totalBytes int64
	err = filepath.WalkDir(contractsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if !checkContext(ctx, problems) {
			return fs.SkipAll
		}
		if walkErr != nil {
			problems.add(toSlashRelative(contractsRoot, path), "walk: %v", walkErr)
			return nil
		}
		rel := toSlashRelative(contractsRoot, path)
		if entry.Type()&os.ModeSymlink != 0 {
			problems.add(rel, "symlinks are forbidden")
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			problems.add(rel, "inspect: %v", err)
			return nil
		}
		if !info.Mode().IsRegular() {
			problems.add(rel, "only regular files are allowed")
			return nil
		}
		fileCount++
		if fileCount > maxContractFiles {
			problems.add("contracts", "repository exceeds %d files", maxContractFiles)
			return fs.SkipAll
		}
		totalBytes += info.Size()
		if totalBytes > maxContractTotalBytes {
			problems.add("contracts", "repository exceeds %d bytes", maxContractTotalBytes)
			return fs.SkipAll
		}
		if info.Size() > maxContractSize {
			problems.add(rel, "file exceeds %d bytes", maxContractSize)
			return nil
		}
		lower := strings.ToLower(rel)
		if !strings.HasSuffix(lower, ".json") && !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".proto") {
			return nil
		}
		data, err := readContractFile(path)
		if err != nil {
			problems.add(rel, "read: %v", err)
			return nil
		}
		file := contractFile{Rel: rel, Abs: path, Data: data}
		switch {
		case strings.HasSuffix(lower, ".schema.json"):
			result.jsonFiles = append(result.jsonFiles, file)
			result.schemaFiles = append(result.schemaFiles, file)
		case strings.HasSuffix(lower, ".json"):
			result.jsonFiles = append(result.jsonFiles, file)
			if strings.HasPrefix(lower, "openapi/") {
				problems.add(rel, "OpenAPI contracts must use .yaml or .yml")
			}
		case strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml"):
			result.yamlFiles = append(result.yamlFiles, file)
			if strings.HasPrefix(lower, "openapi/") {
				result.openAPIFiles = append(result.openAPIFiles, file)
			}
		case strings.HasSuffix(lower, ".proto"):
			result.protoFiles = append(result.protoFiles, file)
		}
		return nil
	})
	if err != nil {
		problems.add("contracts", "walk: %v", err)
	}
	for _, files := range [][]contractFile{result.jsonFiles, result.yamlFiles, result.schemaFiles, result.openAPIFiles, result.protoFiles} {
		sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	}
	if len(result.schemaFiles) == 0 {
		problems.add("contracts", "no JSON Schema files found")
	}
	if len(result.openAPIFiles) == 0 {
		problems.add("contracts/openapi", "no OpenAPI files found")
	}
	if len(result.protoFiles) == 0 {
		problems.add("contracts/protobuf", "no protobuf files found")
	}
	return result
}

func readContractFile(path string) ([]byte, error) {
	// #nosec G304 -- path is produced by the bounded, symlink-rejecting contract tree walk.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("only regular files are allowed")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxContractSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxContractSize {
		return nil, fmt.Errorf("file exceeds %d bytes", maxContractSize)
	}
	return data, nil
}

func toSlashRelative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Sprintf("unresolved:%s", filepath.Base(path))
	}
	return filepath.ToSlash(rel)
}
