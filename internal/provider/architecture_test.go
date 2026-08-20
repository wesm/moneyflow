package provider_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderPackagesDoNotImportStore(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	providerDir := filepath.Dir(filename)
	repoInternalDir := filepath.Dir(providerDir)

	assertNoInternalImport(t, providerDir, func(path string, imported string) bool {
		if imported == "github.com/wesm/moneyflow/internal/store" ||
			strings.HasPrefix(imported, "github.com/wesm/moneyflow/internal/store/") {
			return false
		}
		if filepath.Dir(path) == providerDir &&
			strings.HasPrefix(imported, "github.com/wesm/moneyflow/internal/") {
			return imported == "github.com/wesm/moneyflow/internal/domain"
		}
		return true
	})
	assertNoInternalImport(t, filepath.Join(repoInternalDir, "store"), func(_ string, imported string) bool {
		return imported != "github.com/wesm/moneyflow/internal/provider" &&
			!strings.HasPrefix(imported, "github.com/wesm/moneyflow/internal/provider/")
	})
	assertNoInternalImport(t, filepath.Join(repoInternalDir, "profilecatalog"), func(_ string, imported string) bool {
		if !strings.HasPrefix(imported, "github.com/wesm/moneyflow/internal/") {
			return true
		}
		return imported == "github.com/wesm/moneyflow/internal/domain" ||
			imported == "github.com/wesm/moneyflow/internal/home" ||
			imported == "github.com/wesm/moneyflow/internal/store" ||
			strings.HasPrefix(imported, "github.com/wesm/moneyflow/internal/store/")
	})
}

func TestOnboardingImportsKeepMonarchAtTheCompositionBoundary(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	providerDir := filepath.Dir(filename)
	internalDir := filepath.Dir(providerDir)
	repoDir := filepath.Dir(internalDir)
	onboardingDir := filepath.Join(internalDir, "onboarding")
	compositionFiles := map[string]bool{
		filepath.Join(repoDir, "cmd", "moneyflow", "provider.go"):             true,
		filepath.Join(repoDir, "cmd", "moneyflow", "onboarding_presenter.go"): true,
		// The browser integration binary is a non-production composition root that
		// supplies the same typed runtime with an in-memory provider implementation.
		filepath.Join(internalDir, "tools", "webtestserver", "main.go"): true,
	}
	for path := range compositionFiles {
		_, statErr := os.Stat(path)
		require.NoError(t, statErr, "composition file must exist: %s", path)
	}

	allowed := func(path, imported string) bool {
		if !importsPackageTree(imported, "github.com/wesm/moneyflow/internal/provider/monarch") {
			return true
		}
		return filepath.Dir(path) == onboardingDir || compositionFiles[path]
	}
	assertNoInternalImport(t, internalDir, allowed)
	assertNoInternalImport(t, filepath.Join(repoDir, "cmd"), allowed)
}

func TestRenderersAndWritePlannerDoNotDependOnMonarchOrSQLite(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	providerDir := filepath.Dir(filename)
	internalDir := filepath.Dir(providerDir)
	for _, directory := range []string{"api", "tui", "web"} {
		assertNoInternalImport(t, filepath.Join(internalDir, directory), func(_ string, imported string) bool {
			return !importsPackageTree(imported, "github.com/wesm/moneyflow/internal/provider/monarch")
		})
	}
	appDir := filepath.Join(internalDir, "app")
	for _, name := range []string{"provider_write.go", "provider_write_plan.go"} {
		path := filepath.Join(appDir, name)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imported := range parsed.Imports {
			value, unquoteErr := strconv.Unquote(imported.Path.Value)
			require.NoError(t, unquoteErr)
			assert.False(t, importsPackageTree(value, "github.com/wesm/moneyflow/internal/provider/monarch"))
			assert.False(t, importsPackageTree(value, "github.com/wesm/moneyflow/internal/store/sqlite"))
		}
	}
}

func TestExporterDependencyBoundary(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	providerDir := filepath.Dir(filename)
	internalDir := filepath.Dir(providerDir)
	exporterDir := filepath.Join(internalDir, "exporter")
	assertNoInternalImport(t, exporterDir, func(_ string, imported string) bool {
		if !strings.HasPrefix(imported, "github.com/wesm/moneyflow/internal/") {
			return true
		}
		return imported == "github.com/wesm/moneyflow/internal/app" ||
			imported == "github.com/wesm/moneyflow/internal/home"
	})
	assertNoInternalImport(t, filepath.Join(internalDir, "app"), func(_ string, imported string) bool {
		return !importsPackageTree(imported, "github.com/wesm/moneyflow/internal/exporter")
	})
	for _, directory := range []string{"api", "tui"} {
		assertNoInternalImport(t, filepath.Join(internalDir, directory), func(_ string, imported string) bool {
			return imported != "modernc.org/sqlite" &&
				!importsPackageTree(imported, "github.com/parquet-go/parquet-go")
		})
	}
}

func TestMonarchMutationSurfaceIsLimitedToTransactionUpdateAndDelete(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	monarchDir := filepath.Join(filepath.Dir(filename), "monarch")
	for _, name := range []string{"queries.go", "write.go"} {
		// The filenames are a closed test-owned allowlist beneath the repository package path.
		//nolint:gosec
		contents, err := os.ReadFile(filepath.Join(monarchDir, name))
		require.NoError(t, err)
		text := strings.ToLower(string(contents))
		for _, forbidden := range []string{
			"createtransaction", "createcategory", "updatecategory",
			"deletecategory", "creategroup", "updategroup", "deletegroup",
		} {
			assert.NotContains(t, text, forbidden, "%s must remain outside the slice", forbidden)
		}
	}
	// This is a fixed repository source file beneath the package containing this test.
	//nolint:gosec
	queries, err := os.ReadFile(filepath.Join(monarchDir, "queries.go"))
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(strings.ToLower(string(queries)), "mutation "))
	assert.Contains(t, string(queries), "updateTransaction(input: $input)")
	assert.Contains(t, string(queries), "deleteTransaction(input: $input)")
}

func importsPackageTree(imported string, root string) bool {
	return imported == root || strings.HasPrefix(imported, root+"/")
}

func assertNoInternalImport(
	t *testing.T,
	directory string,
	allowed func(string, string) bool,
) {
	t.Helper()
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		require.NoError(t, parseErr, path)
		for _, imported := range parsed.Imports {
			name, unquoteErr := strconv.Unquote(imported.Path.Value)
			require.NoError(t, unquoteErr)
			assert.True(t, allowed(path, name), "%s imports forbidden package %s", path, name)
		}
		return nil
	})
	require.NoError(t, err)
}
