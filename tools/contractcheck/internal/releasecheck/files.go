package releasecheck

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxEvidenceFiles = 10_000
	maxEvidenceTotal = int64(1 << 30)
	maxManifestSize  = int64(1 << 20)
	maxJSONFileSize  = int64(32 << 20)
	maxArtifactSize  = int64(256 << 20)
	maxLicenseSize   = int64(1 << 20)
)

type evidenceFS struct {
	root     string
	rootInfo os.FileInfo
	files    map[string]os.FileInfo
}

func openEvidenceFS(ctx context.Context, root string) (*evidenceFS, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("evidence root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve evidence root: %w", err)
	}
	rootInfo, err := os.Lstat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect evidence root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("evidence root must be a real directory, not a symlink")
	}

	result := &evidenceFS{root: absRoot, rootInfo: rootInfo, files: make(map[string]os.FileInfo)}
	var count int
	var total int64
	err = filepath.WalkDir(absRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validation interrupted: %w", err)
		}
		if walkErr != nil {
			return walkErr
		}
		if filePath == absRoot {
			return nil
		}
		relative, err := filepath.Rel(absRoot, filePath)
		if err != nil {
			return fmt.Errorf("resolve evidence path: %w", err)
		}
		relative = filepath.ToSlash(relative)
		if _, err := safeRelativePath(relative); err != nil {
			return fmt.Errorf("unsafe evidence entry %q: %w", relative, err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("evidence entry %q is a symlink", relative)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect evidence entry %q: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("evidence entry %q is not a regular file", relative)
		}
		count++
		if count > maxEvidenceFiles {
			return fmt.Errorf("evidence bundle exceeds %d files", maxEvidenceFiles)
		}
		total += info.Size()
		if total > maxEvidenceTotal {
			return fmt.Errorf("evidence bundle exceeds %d bytes", maxEvidenceTotal)
		}
		if info.Size() > maxArtifactSize {
			return fmt.Errorf("evidence entry %q exceeds %d bytes", relative, maxArtifactSize)
		}
		result.files[relative] = info
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan evidence root: %w", err)
	}
	return result, nil
}

func safeRelativePath(value string) (string, error) {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return "", fmt.Errorf("path must be a non-empty repository-relative slash path")
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return "", fmt.Errorf("absolute paths are forbidden")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("control characters are forbidden")
		}
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path traversal is forbidden")
	}
	return cleaned, nil
}

func (bundle *evidenceFS) readBytes(ctx context.Context, relative string, maximum int64) ([]byte, error) {
	file, err := bundle.open(ctx, relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read evidence file %q: %w", relative, err)
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("evidence file %q exceeds %d bytes", relative, maximum)
	}
	if err := bundle.verifyPath(relative, file); err != nil {
		return nil, err
	}
	return data, nil
}

func (bundle *evidenceFS) hashFile(ctx context.Context, relative string, maximum int64) (string, error) {
	file, err := bundle.open(ctx, relative)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(&contextReader{ctx: ctx, reader: file}, maximum+1))
	if err != nil {
		return "", fmt.Errorf("hash evidence file %q: %w", relative, err)
	}
	if written > maximum {
		return "", fmt.Errorf("evidence file %q exceeds %d bytes", relative, maximum)
	}
	if err := bundle.verifyPath(relative, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func (bundle *evidenceFS) open(ctx context.Context, relative string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("validation interrupted: %w", err)
	}
	cleaned, err := safeRelativePath(relative)
	if err != nil {
		return nil, fmt.Errorf("unsafe evidence path %q: %w", relative, err)
	}
	snapshot, exists := bundle.files[cleaned]
	if !exists {
		return nil, fmt.Errorf("evidence file %q is missing", cleaned)
	}
	if err := bundle.checkPath(cleaned); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(bundle.root, filepath.FromSlash(cleaned)))
	if err != nil {
		return nil, fmt.Errorf("open evidence file %q: %w", cleaned, err)
	}
	opened, err := file.Stat()
	if err != nil {
		closeErr := file.Close()
		return nil, errors.Join(
			fmt.Errorf("inspect opened evidence file %q: %w", cleaned, err),
			wrapCloseError(cleaned, closeErr),
		)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(snapshot, opened) {
		closeErr := file.Close()
		return nil, errors.Join(
			fmt.Errorf("evidence file %q changed during validation", cleaned),
			wrapCloseError(cleaned, closeErr),
		)
	}
	return file, nil
}

func wrapCloseError(relative string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close evidence file %q: %w", relative, err)
}

func (bundle *evidenceFS) verifyPath(relative string, opened *os.File) error {
	if err := bundle.checkPath(relative); err != nil {
		return err
	}
	current, err := os.Lstat(filepath.Join(bundle.root, filepath.FromSlash(relative)))
	if err != nil {
		return fmt.Errorf("reinspect evidence file %q: %w", relative, err)
	}
	openedInfo, err := opened.Stat()
	if err != nil {
		return fmt.Errorf("reinspect opened evidence file %q: %w", relative, err)
	}
	if current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, openedInfo) {
		return fmt.Errorf("evidence file %q changed during validation", relative)
	}
	return nil
}

func (bundle *evidenceFS) checkPath(relative string) error {
	rootInfo, err := os.Lstat(bundle.root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || !os.SameFile(bundle.rootInfo, rootInfo) {
		return fmt.Errorf("evidence root changed during validation")
	}
	current := bundle.root
	parts := strings.Split(relative, "/")
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect evidence path %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("evidence path %q contains a symlink", relative)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("evidence path %q has a non-directory parent", relative)
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("evidence path %q is not a regular file", relative)
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, fmt.Errorf("validation interrupted: %w", err)
	}
	return reader.reader.Read(buffer)
}
