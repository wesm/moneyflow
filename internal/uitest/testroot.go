package uitest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wesm/moneyflow/internal/home"
)

// TestRoot removes successful runs and retains synthetic evidence after failure.
// CI sets MONEYFLOW_UI_ARTIFACTS to its private upload directory.
func TestRoot(t testing.TB) string {
	t.Helper()
	parent := os.Getenv("MONEYFLOW_UI_ARTIFACTS")
	if parent == "" {
		parent = filepath.Join(os.TempDir(), "moneyflow-ui-failures")
	}
	parent, err := filepath.Abs(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err = home.EnsurePrivateDirectory(parent); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(parent, "scenario-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Synthetic UI artifacts retained at %s", root)
			return
		}
		if err := os.RemoveAll(root); err != nil { // #nosec G703 -- root is the directory returned by MkdirTemp above.
			t.Errorf("remove successful scenario: %v", err)
		}
	})
	return root
}
