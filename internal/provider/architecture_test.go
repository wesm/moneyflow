package provider_test

import (
	"go/parser"
	"go/token"
	"io/fs"
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
