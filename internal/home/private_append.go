package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AppendPrivateJSONL durably appends complete lines to a private regular file.
// The caller must serialize appenders. An incomplete previous append is preserved
// and stops further writes so an unknown outcome cannot disappear into a bad line.
func AppendPrivateJSONL(path string, contents []byte) (err error) {
	if len(contents) == 0 {
		return nil
	}
	if contents[len(contents)-1] != '\n' {
		return errors.New("append private JSONL: incomplete record")
	}
	if err = rejectNonRegularTarget(path, "append private JSONL"); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	file, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("append private JSONL: target is not regular")
	}
	if err = enforcePrivateFile(path); err != nil {
		return err
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err = file.ReadAt(last[:], info.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			return errors.New("append private JSONL: existing final record is incomplete; preserve and repair the audit file before retrying")
		}
	}
	if _, err = file.Write(contents); err != nil {
		return fmt.Errorf("append private JSONL: write: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("append private JSONL: sync: %w", err)
	}
	return SyncPrivateDirectory(filepath.Dir(path))
}
