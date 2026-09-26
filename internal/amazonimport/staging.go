package amazonimport

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

const managedStageDirectory = ".amazon-import-stages"

func validateUploads(files []Upload, limits amazon.Limits) error {
	if len(files) == 0 {
		return newError(CodeImportEmpty, errors.New("no upload files"))
	}
	if len(files) > limits.Files {
		return newError(CodeImportTooLarge, errors.New("too many upload files"))
	}
	names := make(map[string]struct{}, len(files))
	for _, upload := range files {
		name := filepath.ToSlash(upload.RelativeName)
		if upload.Reader == nil || name == "" || name == "." || strings.HasPrefix(name, "/") ||
			name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") ||
			!strings.HasPrefix(filepath.Base(name), "Retail.OrderHistory.") ||
			!strings.HasSuffix(filepath.Base(name), ".csv") {
			return newError(CodeImportInvalid, errors.New("upload name is invalid"))
		}
		if _, duplicate := names[name]; duplicate {
			return newError(CodeImportInvalid, errors.New("upload name is duplicated"))
		}
		names[name] = struct{}{}
	}
	return nil
}

func stageUploads(
	ctx context.Context,
	root, attemptID string,
	files []Upload,
	limits amazon.Limits,
	now time.Time,
) ([]amazon.SourceFile, string, error) {
	if err := validateUploads(files, limits); err != nil {
		return nil, "", err
	}
	parent, err := home.EnsurePrivateSubdirectory(root, managedStageDirectory)
	if err != nil {
		return nil, "", newError(CodeImportInvalid, err)
	}
	if err = removeStaleStages(parent, now.Add(-attemptIdleLimit)); err != nil {
		return nil, "", newError(CodeImportInvalid, err)
	}
	stageDir, err := home.EnsurePrivateSubdirectory(parent, attemptID)
	if err != nil {
		return nil, "", newError(CodeImportInvalid, err)
	}
	clean := false
	defer func() {
		if !clean {
			_ = os.RemoveAll(stageDir)
		}
	}()
	ordered := append([]Upload(nil), files...)
	slices.SortFunc(ordered, func(left, right Upload) int { return strings.Compare(left.RelativeName, right.RelativeName) })
	staged := make([]amazon.SourceFile, 0, len(ordered))
	contents := make(map[[sha256.Size]byte]struct{}, len(ordered))
	var total int64
	for index, upload := range ordered {
		if err = ctx.Err(); err != nil {
			return nil, "", newError(CodeImportCanceled, err)
		}
		path := filepath.Join(stageDir, fmt.Sprintf("%06d.csv", index))
		// The path is a coordinator-generated numeric basename beneath the validated stage root.
		//nolint:gosec
		file, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if openErr != nil {
			return nil, "", newError(CodeImportInvalid, openErr)
		}
		digest := sha256.New()
		written, copyErr := io.Copy(
			io.MultiWriter(file, digest),
			io.LimitReader(&contextUploadReader{ctx: ctx, source: upload.Reader}, limits.BytesPerFile+1),
		)
		syncErr := file.Sync()
		closeErr := file.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil {
			return nil, "", newError(CodeImportInvalid, errors.Join(copyErr, syncErr, closeErr))
		}
		if written > limits.BytesPerFile || total > limits.TotalBytes-written {
			return nil, "", newError(CodeImportTooLarge, errors.New("upload bytes exceed limit"))
		}
		var digestKey [sha256.Size]byte
		copy(digestKey[:], digest.Sum(nil))
		if _, duplicate := contents[digestKey]; duplicate {
			return nil, "", newError(CodeImportInvalid, errors.New("upload content is duplicated"))
		}
		contents[digestKey] = struct{}{}
		total += written
		staged = append(staged, amazon.SourceFile{RelativeName: filepath.ToSlash(upload.RelativeName), Path: path})
	}
	clean = true
	return staged, stageDir, nil
}

func removeStage(path string) error {
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if filepath.Base(parent) == managedStageDirectory {
		_ = os.Remove(parent)
	}
	return nil
}

func removeStaleStages(parent string, cutoff time.Time) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.ModTime().Before(cutoff) {
			if err = os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

type contextUploadReader struct {
	ctx    context.Context
	source io.Reader
}

func (reader *contextUploadReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.source.Read(buffer)
}
