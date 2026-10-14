// Package access names the authority each moneyflow operation needs.
package access

// Class is the authority one API operation or MCP tool needs.
type Class string

const (
	// Read changes no profile data or workflow state: no database rows, provider state,
	// attempts or exported files. Serving it may open the profile, which can create lock and
	// SQLite side files, restrict file permissions and fill in-process caches.
	Read Class = "read"
	// Stage changes only local data or state.
	Stage Class = "stage"
	// Commit sends edits to a provider or confirms a destructive change.
	Commit Class = "commit"
)

// Valid reports whether c is one of the three classes.
func (c Class) Valid() bool {
	return c == Read || c == Stage || c == Commit
}
