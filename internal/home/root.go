// Package home resolves and protects the Moneyflow home and its profile locations.
package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const databaseName = "moneyflow.db"

// ErrPythonData reports a selected root that holds data from the retired Python app.
var ErrPythonData = errors.New("moneyflow home holds Python app data")

// pythonMarkers are files the retired Python app kept at the top of its data folder. Go never
// writes these names in a root. Python wrote moneyflow.log on every start and accounts.json for
// every account, so these files find any folder the Python app used.
var pythonMarkers = []string{
	"accounts.json", "config.yaml", "credentials.enc", "merchants.json", "moneyflow.log",
}

// Paths names the canonical profile root and its single SQLite database.
type Paths struct {
	Root     string
	Database string
}

// ResolveRoot selects an explicit root, MONEYFLOW_HOME, or ~/.moneyflow. It fails with
// ErrPythonData, before anything is created, when the root holds Python app data.
func ResolveRoot(
	explicit string,
	lookupEnv func(string) (string, bool),
	userHome string,
) (Paths, error) {
	root := explicit
	if root == "" && lookupEnv != nil {
		if value, ok := lookupEnv("MONEYFLOW_HOME"); ok && value != "" {
			root = value
		}
	}
	if root == "" {
		if userHome == "" {
			return Paths{}, errors.New("resolve profile root: user home is empty")
		}
		root = filepath.Join(userHome, ".moneyflow")
	}
	canonical, err := canonicalRoot(root)
	if err != nil {
		return Paths{}, err
	}
	if err = refusePythonData(canonical); err != nil {
		return Paths{}, err
	}
	return Paths{Root: canonical, Database: filepath.Join(canonical, databaseName)}, nil
}

func refusePythonData(root string) error {
	for _, marker := range pythonMarkers {
		_, err := os.Lstat(filepath.Join(root, marker))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("resolve profile root: inspect %s: %w", marker, err)
		}
		return fmt.Errorf(
			"%w: %s contains %s from the Python app, and Go Moneyflow cannot share that folder. "+
				"Stop Moneyflow and move the folder aside with: mv %s %s. Then see "+
				"\"Moving from Python to Go\" (docs/getting-started/transition.md)",
			ErrPythonData, root, marker, shellQuote(root), shellQuote(root+"-python"),
		)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// PrepareDatabase creates the managed root and database with private platform permissions.
func PrepareDatabase(paths Paths) error {
	if paths.Root == "" || paths.Database == "" || !filepath.IsAbs(paths.Root) || !filepath.IsAbs(paths.Database) {
		return errors.New("prepare profile database: paths must be absolute")
	}
	if filepath.Clean(paths.Database) != filepath.Join(filepath.Clean(paths.Root), databaseName) {
		return errors.New("prepare profile database: database is outside the selected root")
	}
	if err := os.MkdirAll(paths.Root, 0o700); err != nil {
		return fmt.Errorf("prepare profile database: create root: %w", err)
	}
	if err := enforcePrivateDirectory(paths.Root); err != nil {
		return err
	}
	if info, err := os.Lstat(paths.Database); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("prepare profile database: database is a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return errors.New("prepare profile database: database is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("prepare profile database: inspect database: %w", err)
	}

	root, err := os.OpenRoot(paths.Root)
	if err != nil {
		return fmt.Errorf("prepare profile database: open root: %w", err)
	}
	defer func() { _ = root.Close() }()
	database, err := root.OpenFile(databaseName, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return fmt.Errorf("prepare profile database: create database: %w", err)
	}
	info, statErr := database.Stat()
	closeErr := database.Close()
	if statErr != nil {
		return fmt.Errorf("prepare profile database: inspect created database: %w", statErr)
	}
	if !info.Mode().IsRegular() {
		return errors.New("prepare profile database: created database is not a regular file")
	}
	if closeErr != nil {
		return fmt.Errorf("prepare profile database: close database: %w", closeErr)
	}
	if err := enforcePrivateFile(paths.Database); err != nil {
		return err
	}
	return nil
}

// PreparePrivateRoot validates the existing path chain before creating and restricting a root.
func PreparePrivateRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return errors.New("prepare private root: path must be absolute")
	}
	canonical, err := canonicalRoot(root)
	if err != nil {
		return err
	}
	existing := filepath.Clean(root)
	for {
		if _, err = os.Lstat(existing); err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("prepare private root: inspect ancestor: %w", err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return errors.New("prepare private root: no existing ancestor")
		}
		existing = parent
	}
	if err = validateTrustedRootAncestors(existing, filepath.Clean(root)); err != nil {
		return err
	}
	if err = validateRootSpelling(root, canonical); err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("prepare private root: create: %w", err)
	}
	if err = enforcePrivateDirectory(root); err != nil {
		return err
	}
	return nil
}

// EnsurePrivateSubdirectory creates a fixed relative path without following an intermediate link.
func EnsurePrivateSubdirectory(root string, components ...string) (string, error) {
	if err := PreparePrivateRoot(root); err != nil {
		return "", err
	}
	current := filepath.Clean(root)
	for _, component := range components {
		if component == "" || component == "." || component == ".." || filepath.Base(component) != component {
			return "", errors.New("prepare private subdirectory: invalid component")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(current, 0o700); err != nil {
				return "", fmt.Errorf("prepare private subdirectory: create: %w", err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", fmt.Errorf("prepare private subdirectory: inspect: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("prepare private subdirectory: path is a symbolic link")
		}
		if !info.IsDir() {
			return "", errors.New("prepare private subdirectory: path is not a directory")
		}
		if err = enforcePrivateDirectory(current); err != nil {
			return "", err
		}
	}
	return current, nil
}

// canonicalRoot resolves the existing prefix while retaining a missing suffix exactly.
func canonicalRoot(input string) (string, error) {
	target := input
	if !filepath.IsAbs(target) {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve profile root: working directory: %w", err)
		}
		target = workingDirectory + string(os.PathSeparator) + target
	}
	current := strings.TrimRight(target, string(os.PathSeparator))
	if current == "" {
		current = string(os.PathSeparator)
	}
	var missing []string
	for {
		_, err := os.Lstat(current) //nolint:gosec // the caller-selected profile root is the intended target.
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve profile root: inspect existing ancestor: %w", err)
		}
		parent, component := rawPathParent(current)
		if parent == current || component == "" {
			return "", errors.New("resolve profile root: no existing ancestor")
		}
		if component == "." || component == ".." {
			return "", errors.New("resolve profile root: traversal follows a missing component")
		}
		missing = append(missing, component)
		current = parent
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", fmt.Errorf("resolve profile root: canonicalize existing ancestor: %w", err)
	}
	slices.Reverse(missing)
	return filepath.Join(append([]string{resolved}, missing...)...), nil
}

func rawPathParent(path string) (string, string) {
	volume := filepath.VolumeName(path)
	rest := strings.TrimRight(path[len(volume):], string(os.PathSeparator))
	index := strings.LastIndex(rest, string(os.PathSeparator))
	if index < 0 {
		return path, ""
	}
	component := rest[index+1:]
	parentRest := strings.TrimRight(rest[:index], string(os.PathSeparator))
	if parentRest == "" {
		parentRest = string(os.PathSeparator)
	}
	return volume + parentRest, component
}
