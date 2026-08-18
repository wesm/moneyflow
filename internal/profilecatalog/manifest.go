package profilecatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
)

const (
	// ManifestVersion is the only catalog manifest version understood by this binary.
	ManifestVersion = uint16(1)
	// ManifestFilename is the fixed profile-root metadata filename.
	ManifestFilename = "profile.json"
	// ManifestMaximumBytes bounds metadata reads before JSON decoding.
	ManifestMaximumBytes = 16 * 1024

	maximumDisplayNameRunes = 80
	maximumDisplayNameBytes = 320
	canonicalManifestTime   = "2006-01-02T15:04:05.000000000Z"
)

// Manifest is the exact version-one local profile metadata contract.
type Manifest struct {
	ManifestVersion  uint16
	ProfileID        string
	DisplayName      string
	ProviderKind     string
	CreatedAt        time.Time
	CreatedByVersion string
}

type manifestDocument struct {
	ManifestVersion  uint16 `json:"manifest_version"`
	ProfileID        string `json:"profile_id"`
	DisplayName      string `json:"display_name"`
	ProviderKind     string `json:"provider_kind"`
	CreatedAt        string `json:"created_at"`
	CreatedByVersion string `json:"created_by_version"`
}

var manifestFields = map[string]struct{}{
	"manifest_version":   {},
	"profile_id":         {},
	"display_name":       {},
	"provider_kind":      {},
	"created_at":         {},
	"created_by_version": {},
}

// ProbeManifestVersion reads only the bounded JSON envelope and its integer version.
func ProbeManifestVersion(path string) (uint16, error) {
	fields, err := readManifestObject(path)
	if err != nil {
		return 0, err
	}
	raw, ok := fields["manifest_version"]
	if !ok {
		return 0, newError(CodeProfileInvalid, errors.New("manifest version is missing"))
	}
	var version uint16
	if err = json.Unmarshal(raw, &version); err != nil {
		return 0, newError(CodeProfileInvalid, errors.New("manifest version is not an integer"))
	}
	return version, nil
}

// ReadManifest validates and returns one exact version-one manifest.
func ReadManifest(path string) (Manifest, error) {
	return readManifest(path, filepath.Base(filepath.Dir(path)))
}

func readLegacyManifest(path string) (Manifest, error) {
	return readManifest(path, "")
}

func readManifest(path string, expectedDirectoryID string) (Manifest, error) {
	fields, err := readManifestObject(path)
	if err != nil {
		return Manifest{}, err
	}
	version, err := manifestVersion(fields)
	if err != nil {
		return Manifest{}, err
	}
	if version != ManifestVersion {
		return Manifest{}, newError(CodeManifestUnsupported, errors.New("unknown manifest version"))
	}
	if len(fields) != len(manifestFields) {
		return Manifest{}, newError(CodeProfileInvalid, errors.New("manifest field set is invalid"))
	}
	for field := range fields {
		if _, ok := manifestFields[field]; !ok {
			return Manifest{}, newError(CodeProfileInvalid, errors.New("manifest field is unknown"))
		}
	}
	document := manifestDocument{ManifestVersion: version}
	decoders := map[string]any{
		"profile_id":         &document.ProfileID,
		"display_name":       &document.DisplayName,
		"provider_kind":      &document.ProviderKind,
		"created_at":         &document.CreatedAt,
		"created_by_version": &document.CreatedByVersion,
	}
	for field, target := range decoders {
		raw, ok := fields[field]
		if !ok {
			return Manifest{}, newError(CodeProfileInvalid, errors.New("manifest field is missing"))
		}
		if err = json.Unmarshal(raw, target); err != nil {
			return Manifest{}, newError(CodeProfileInvalid, errors.New("manifest field has wrong type"))
		}
	}
	createdAt, err := time.Parse(canonicalManifestTime, document.CreatedAt)
	if err != nil || document.CreatedAt != createdAt.Format(canonicalManifestTime) {
		return Manifest{}, newError(CodeProfileInvalid, errors.New("manifest time is not canonical"))
	}
	manifest := Manifest{
		ManifestVersion:  document.ManifestVersion,
		ProfileID:        document.ProfileID,
		DisplayName:      document.DisplayName,
		ProviderKind:     document.ProviderKind,
		CreatedAt:        createdAt,
		CreatedByVersion: document.CreatedByVersion,
	}
	if err = validateManifest(manifest, expectedDirectoryID); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// NormalizeDisplayName returns the canonical display value and its catalog collision key.
func NormalizeDisplayName(value string) (string, string, error) {
	normalized, err := domain.NormalizeDisplayLabel(value)
	if err != nil {
		return "", "", err
	}
	if utf8.RuneCountInString(normalized) > maximumDisplayNameRunes {
		return "", "", errors.New("profile display name exceeds character limit")
	}
	if len(normalized) > maximumDisplayNameBytes {
		return "", "", errors.New("profile display name exceeds byte limit")
	}
	key, err := domain.CollisionKey(normalized)
	if err != nil {
		return "", "", err
	}
	return normalized, key, nil
}

func writeManifest(path string, manifest Manifest) error {
	return writeManifestForDirectory(path, manifest, filepath.Base(filepath.Dir(path)))
}

func writeLegacyManifest(path string, manifest Manifest) error {
	return writeManifestForDirectory(path, manifest, "")
}

func writeManifestForDirectory(path string, manifest Manifest, expectedDirectoryID string) error {
	if err := validateManifest(manifest, expectedDirectoryID); err != nil {
		return err
	}
	contents, err := marshalManifest(manifest)
	if err != nil {
		return err
	}
	if len(contents) > ManifestMaximumBytes {
		return newError(CodeProfileInvalid, errors.New("manifest exceeds maximum size"))
	}
	if err = home.WritePrivateFile(path, contents); err != nil {
		return newError(CodeProfileInvalid, err)
	}
	return nil
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	if err := validateManifest(manifest, ""); err != nil {
		return nil, err
	}
	document := manifestDocument{
		ManifestVersion:  manifest.ManifestVersion,
		ProfileID:        manifest.ProfileID,
		DisplayName:      manifest.DisplayName,
		ProviderKind:     manifest.ProviderKind,
		CreatedAt:        manifest.CreatedAt.Format(canonicalManifestTime),
		CreatedByVersion: manifest.CreatedByVersion,
	}
	contents, err := json.Marshal(document)
	if err != nil {
		return nil, newError(CodeProfileInvalid, err)
	}
	return append(contents, '\n'), nil
}

func validateManifest(manifest Manifest, directory string) error {
	if manifest.ManifestVersion != ManifestVersion {
		return newError(CodeManifestUnsupported, errors.New("unknown manifest version"))
	}
	if !ValidProfileID(manifest.ProfileID) {
		return newError(CodeProfileInvalid, errors.New("profile ID is not canonical"))
	}
	if ValidProfileID(directory) && directory != manifest.ProfileID {
		return newError(CodeProfileInvalid, errors.New("profile ID does not match its directory"))
	}
	name, _, err := NormalizeDisplayName(manifest.DisplayName)
	if err != nil || name != manifest.DisplayName {
		return newError(CodeProfileInvalid, errors.New("display name is not canonical"))
	}
	if manifest.ProviderKind != "monarch" && manifest.ProviderKind != "local" {
		return newError(CodeProfileInvalid, errors.New("provider kind is unsupported"))
	}
	if manifest.CreatedAt.IsZero() || manifest.CreatedAt.Location() != time.UTC {
		return newError(CodeProfileInvalid, errors.New("creation time is not UTC"))
	}
	if err = validateVersionString(manifest.CreatedByVersion); err != nil {
		return newError(CodeProfileInvalid, err)
	}
	return nil
}

func validateVersionString(value string) error {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return errors.New("creator version is invalid")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New("creator version is invalid")
		}
	}
	return nil
}

func manifestVersion(fields map[string]json.RawMessage) (uint16, error) {
	raw, ok := fields["manifest_version"]
	if !ok {
		return 0, newError(CodeProfileInvalid, errors.New("manifest version is missing"))
	}
	var version uint16
	if err := json.Unmarshal(raw, &version); err != nil {
		return 0, newError(CodeProfileInvalid, errors.New("manifest version is not an integer"))
	}
	return version, nil
}

func readManifestObject(path string) (map[string]json.RawMessage, error) {
	contents, err := home.ReadPrivateFile(path, int64(ManifestMaximumBytes))
	if err != nil {
		return nil, newError(CodeProfileInvalid, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	token, err := decoder.Token()
	if err != nil {
		return nil, newError(CodeProfileInvalid, errors.New("manifest is not JSON"))
	}
	opening, ok := token.(json.Delim)
	if !ok || opening != '{' {
		return nil, newError(CodeProfileInvalid, errors.New("manifest is not an object"))
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, newError(CodeProfileInvalid, errors.New("manifest key is invalid"))
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, newError(CodeProfileInvalid, errors.New("manifest key is not text"))
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, newError(CodeProfileInvalid, errors.New("manifest contains a duplicate field"))
		}
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil {
			return nil, newError(CodeProfileInvalid, errors.New("manifest field is invalid"))
		}
		fields[key] = raw
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, newError(CodeProfileInvalid, errors.New("manifest object is incomplete"))
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, newError(CodeProfileInvalid, errors.New("manifest has trailing JSON"))
	}
	return fields, nil
}
