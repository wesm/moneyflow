package bankcsv

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SourceFile is a discovered regular file and its display name.
type SourceFile struct{ Path, RelativeName string }

// Discover accepts an explicit file or recursively finds mapping matches without following symlinks.
func Discover(ctx context.Context, path string, mapping Mapping, limits Limits) ([]SourceFile, error) {
	if err := mapping.Validate(); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	var files []SourceFile
	var total int64
	add := func(path, relative string, info fs.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("CSV source must be a regular file, not a symlink or device")
		}
		if len(files) >= limits.Files || info.Size() > limits.BytesPerFile || info.Size() > limits.TotalBytes-total {
			return errors.New("CSV discovery limit exceeded")
		}
		total += info.Size()
		files = append(files, SourceFile{path, relative})
		return nil
	}
	if !info.IsDir() {
		err = add(absolute, filepath.Base(absolute), info)
	} else {
		err = filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			matched, err := filepath.Match(mapping.Pattern, entry.Name())
			if err != nil || !matched {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(absolute, path)
			if err != nil {
				return err
			}
			return add(path, relative, info)
		})
	}
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("no matching CSV files found")
	}
	slices.SortFunc(files, func(a, b SourceFile) int { return strings.Compare(a.RelativeName, b.RelativeName) })
	return files, nil
}
