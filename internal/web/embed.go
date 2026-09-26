// Package web validates and serves the embedded Moneyflow browser application.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

//go:embed all:dist
var embeddedDistribution embed.FS

var contentHashPattern = regexp.MustCompile(
	`^assets/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9._-]+-[A-Za-z0-9_-]{8,}\.[a-z0-9]+$`,
)

type manifestEntry struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	Assets  []string `json:"assets"`
	IsEntry bool     `json:"isEntry"`
}

type distribution struct {
	filesystem fs.FS
	index      []byte
	assets     map[string]struct{}
}

// ValidateDistribution verifies that fsys contains one complete Vite production build under dist.
func ValidateDistribution(fsys fs.FS) error {
	_, err := validateDistribution(fsys)
	return err
}

func validateDistribution(fsys fs.FS) (*distribution, error) {
	if fsys == nil {
		return nil, errors.New("validate web distribution: filesystem is required")
	}
	var names []string
	err := fs.WalkDir(fsys, "dist", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link is forbidden: %s", name)
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(name, "dist/")
		if !fs.ValidPath(relative) || strings.HasSuffix(relative, ".map") {
			return fmt.Errorf("unsafe distribution file: %s", relative)
		}
		names = append(names, relative)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("validate web distribution: %w", err)
	}
	sort.Strings(names)
	files := make(map[string]struct{}, len(names))
	for _, name := range names {
		files[name] = struct{}{}
	}
	for _, required := range []string{"index.html", ".vite/manifest.json", ".moneyflow-production.json"} {
		if _, ok := files[required]; !ok {
			return nil, fmt.Errorf("validate web distribution: missing %s", required)
		}
	}

	markerBytes, err := fs.ReadFile(fsys, "dist/.moneyflow-production.json")
	if err != nil {
		return nil, fmt.Errorf("validate web distribution marker: %w", err)
	}
	var marker map[string]any
	if err := json.Unmarshal(markerBytes, &marker); err != nil {
		return nil, fmt.Errorf("validate web distribution marker: %w", err)
	}
	if len(marker) != 3 || marker["schema_version"] != float64(1) ||
		marker["kind"] != "moneyflow-production" || marker["entry"] != "index.html" {
		return nil, errors.New("validate web distribution: invalid production marker")
	}

	manifestBytes, err := fs.ReadFile(fsys, "dist/.vite/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("validate web distribution manifest: %w", err)
	}
	manifest := make(map[string]manifestEntry)
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("validate web distribution manifest: %w", err)
	}
	if len(manifest) == 0 {
		return nil, errors.New("validate web distribution: empty manifest")
	}
	assets := make(map[string]struct{})
	entryCount := 0
	for _, entry := range manifest {
		if entry.IsEntry {
			entryCount++
		}
		for _, name := range append(append([]string{entry.File}, entry.CSS...), entry.Assets...) {
			if name == "" {
				continue
			}
			if !contentHashPattern.MatchString(name) {
				return nil, fmt.Errorf("validate web distribution: malformed content hash: %s", name)
			}
			if _, ok := files[name]; !ok {
				return nil, fmt.Errorf("validate web distribution: missing manifest asset: %s", name)
			}
			assets[name] = struct{}{}
		}
	}
	if entryCount != 1 {
		return nil, fmt.Errorf("validate web distribution: expected one entry, got %d", entryCount)
	}
	for _, name := range names {
		if name == "index.html" || name == ".vite/manifest.json" || name == ".moneyflow-production.json" {
			continue
		}
		if _, ok := assets[name]; !ok {
			return nil, fmt.Errorf("validate web distribution: unreferenced asset: %s", name)
		}
	}

	index, err := fs.ReadFile(fsys, "dist/index.html")
	if err != nil {
		return nil, fmt.Errorf("validate web distribution index: %w", err)
	}
	if strings.Count(string(index), basePathPlaceholder) != 1 {
		return nil, errors.New("validate web distribution: index must contain one base-path placeholder")
	}
	if strings.Count(string(index), baseHrefPlaceholder) != 1 {
		return nil, errors.New("validate web distribution: index must contain one base-href placeholder")
	}
	if strings.Count(string(index), mutationTokenPlaceholder) != 1 {
		return nil, errors.New("validate web distribution: index must contain one mutation-token placeholder")
	}
	if strings.Count(string(index), canonicalURLPlaceholder) != 1 {
		return nil, errors.New("validate web distribution: index must contain one canonical-URL placeholder")
	}
	if strings.Count(string(index), originWarningPlaceholder) != 1 {
		return nil, errors.New("validate web distribution: index must contain one origin-warning placeholder")
	}
	if strings.Contains(string(index), "/src/") || strings.Contains(string(index), "/@vite/") {
		return nil, errors.New("validate web distribution: index is a compilation stub")
	}
	distFS, err := fs.Sub(fsys, "dist")
	if err != nil {
		return nil, fmt.Errorf("open web distribution: %w", err)
	}
	return &distribution{filesystem: distFS, index: index, assets: assets}, nil
}

func isSafeDistributionName(name string) bool {
	return fs.ValidPath(name) && path.Clean(name) == name && !strings.Contains(name, "\\")
}
