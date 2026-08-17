package home

import "path/filepath"

const profilesDirectoryName = "profiles"

// CatalogPaths names the catalog root, nested profile directory, and legacy profile.
type CatalogPaths struct {
	Root     string
	Profiles string
}

// ResolveCatalogRoot selects and prepares the private Go v2 catalog root.
func ResolveCatalogRoot(
	explicit string,
	lookupEnv func(string) (string, bool),
	userHome string,
) (CatalogPaths, error) {
	legacy, err := ResolveRoot(explicit, lookupEnv, userHome)
	if err != nil {
		return CatalogPaths{}, err
	}
	if err = PreparePrivateRoot(legacy.Root); err != nil {
		return CatalogPaths{}, err
	}
	return CatalogPaths{
		Root:     legacy.Root,
		Profiles: filepath.Join(legacy.Root, profilesDirectoryName),
	}, nil
}

// LegacyProfile returns the root-level profile retained from the original Go v2 layout.
func (paths CatalogPaths) LegacyProfile() Paths {
	return Paths{Root: paths.Root, Database: filepath.Join(paths.Root, databaseName)}
}
