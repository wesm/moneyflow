package amazon

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func DiscoverDirectory(ctx context.Context, root string, limits Limits) ([]SourceFile, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, newError(CodeInvalid, ErrInvalid)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, newError(CodeInvalid, ErrInvalid)
	}
	files := make([]SourceFile, 0)
	contents := make(map[[sha256.Size]byte]struct{})
	var total int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return newError(CodeInvalid, ErrInvalid)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		entryInfo, statErr := os.Lstat(path)
		if statErr != nil || entryInfo.Mode()&os.ModeSymlink != 0 {
			return newError(CodeInvalid, ErrInvalid)
		}
		if entry.IsDir() {
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			return newError(CodeInvalid, ErrInvalid)
		}
		if !strings.HasPrefix(filepath.Base(path), "Retail.OrderHistory.") ||
			!strings.HasSuffix(filepath.Base(path), ".csv") {
			return nil
		}
		if len(files) == limits.Files {
			return newError(CodeTooLarge, ErrTooLarge)
		}
		if entryInfo.Size() > limits.BytesPerFile || total > limits.TotalBytes-entryInfo.Size() {
			return newError(CodeTooLarge, ErrTooLarge)
		}
		digest, digestErr := digestFile(ctx, path, limits.BytesPerFile)
		if digestErr != nil {
			return digestErr
		}
		if _, duplicate := contents[digest]; duplicate {
			return newError(CodeInvalid, ErrInvalid)
		}
		contents[digest] = struct{}{}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return newError(CodeInvalid, ErrInvalid)
		}
		files = append(files, SourceFile{RelativeName: filepath.ToSlash(relative), Path: path})
		total += entryInfo.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, newError(CodeEmpty, ErrEmpty)
	}
	slices.SortFunc(files, func(left, right SourceFile) int {
		return strings.Compare(left.RelativeName, right.RelativeName)
	})
	return files, nil
}

func digestFile(ctx context.Context, path string, limit int64) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	file, err := os.Open(path) // #nosec G304 -- path came from the rooted, no-symlink discovery walk.
	if err != nil {
		return zero, newError(CodeInvalid, ErrInvalid)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(&contextReader{ctx: ctx, reader: file}, limit+1))
	if err != nil {
		return zero, fmt.Errorf("digest Amazon source: %w", err)
	}
	if read > limit {
		return zero, newError(CodeTooLarge, ErrTooLarge)
	}
	copy(zero[:], hash.Sum(nil))
	return zero, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	read, err := reader.reader.Read(buffer)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return read, reader.ctx.Err()
	}
	return read, err
}
