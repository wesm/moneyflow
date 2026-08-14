// Command checkfmt reports Go source files whose bytes differ from gofmt output.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	files, err := checkPaths(os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, file := range files {
		_, _ = fmt.Fprintln(os.Stderr, file)
	}
	if len(files) > 0 {
		os.Exit(1)
	}
}

func checkPaths(paths []string) ([]string, error) {
	var unformatted []string
	for _, root := range paths {
		// #nosec G703 -- roots are explicit repository paths passed by the Make target.
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}
			source, err := os.ReadFile(path) //nolint:gosec // paths are explicit command arguments.
			if err != nil {
				return err
			}
			formatted, err := format.Source(source)
			if err != nil {
				return fmt.Errorf("format %s: %w", path, err)
			}
			if !bytes.Equal(source, formatted) {
				unformatted = append(unformatted, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("check format under %s: %w", root, err)
		}
	}
	sort.Strings(unformatted)
	return unformatted, nil
}
