package profilecatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const (
	// RecoveryMarkerVersion is the only recovery marker understood by this binary.
	RecoveryMarkerVersion = uint16(1)
	recoveryTimestamp     = "20060102T150405.000000000Z"
)

// RecoveryFaultPoint identifies a durable boundary used by recovery fault tests.
type RecoveryFaultPoint string

// Durable recovery boundaries.
const (
	RecoveryAfterMarkerWrite   RecoveryFaultPoint = "after_marker_write"
	RecoveryAfterWALMove       RecoveryFaultPoint = "after_wal_move"
	RecoveryAfterSHMMove       RecoveryFaultPoint = "after_shm_move"
	RecoveryAfterMainMove      RecoveryFaultPoint = "after_main_move"
	RecoveryAfterEmptyCreate   RecoveryFaultPoint = "after_empty_create"
	RecoveryAfterSchemaInstall RecoveryFaultPoint = "after_schema_install"
	RecoveryAfterVerification  RecoveryFaultPoint = "after_verification"
	RecoveryAfterMarkerRemoval RecoveryFaultPoint = "after_marker_removal"
)

// RecoveryPlan is the exact destructive action shown for confirmation.
type RecoveryPlan struct {
	ProfileKey   string
	ProfileID    string
	BackupPath   string
	StartedAt    time.Time
	OriginalCode store.ErrorCode
	InProgress   bool
}

// RecoveryRequest confirms one previously derived recovery plan.
type RecoveryRequest struct {
	Plan      RecoveryPlan
	Confirmed bool
}

// RecoveryResult identifies the retained backup after a successful roll-forward.
type RecoveryResult struct {
	BackupPath string
}

type recoveryMarker struct {
	MarkerVersion    uint16
	ProfileID        string
	StartedAt        time.Time
	CreatedByVersion string
	OriginalCode     store.ErrorCode
}

type recoveryMarkerDocument struct {
	MarkerVersion      uint16          `json:"marker_version"`
	ProfileID          string          `json:"profile_id"`
	StartedAt          string          `json:"started_at"`
	ApplicationVersion string          `json:"application_version"`
	OriginalCode       store.ErrorCode `json:"original_store_code"`
}

type recoveryActionKind uint8

const (
	recoveryActionRefuse recoveryActionKind = iota
	recoveryActionMoveOld
	recoveryActionInstall
	recoveryActionFinish
)

type recoveryOriginal uint8

const (
	recoveryOriginalMissing recoveryOriginal = iota
	recoveryOriginalEmpty
	recoveryOriginalOlder
	recoveryOriginalNewer
	recoveryOriginalCurrentPristine
	recoveryOriginalCurrentPopulated
	recoveryOriginalCorrupt
)

type recoveryState struct {
	backupMain   bool
	originalMain bool
	original     recoveryOriginal
}

var recoveryMarkerFields = []string{
	"application_version",
	"marker_version",
	"original_store_code",
	"profile_id",
	"started_at",
}

// RecoveryPlan returns the exact backup destination and recoverable storage condition.
func (catalog *Catalog) RecoveryPlan(ctx context.Context, selector string) (RecoveryPlan, error) {
	entry, err := catalog.recoveryEntry(ctx, selector)
	if err != nil {
		return RecoveryPlan{}, err
	}
	if entry.Status == StatusRequiresNewer || entry.Status == StatusManifestUnsupported {
		return RecoveryPlan{}, newError(CodeRecoveryUnavailable, errors.New("profile is not recoverable"))
	}
	active, found, err := scanActiveRecovery(entry.Root, entry.Key)
	if err != nil {
		return RecoveryPlan{}, err
	}
	if found {
		return active, nil
	}
	code, err := recoverableOriginalCode(ctx, entry.ProfilePaths())
	if err != nil {
		return RecoveryPlan{}, err
	}
	startedAt := catalog.now().UTC()
	profileID := entry.ID
	if profileID == "" {
		profileID, err = NewProfileID(catalog.random)
		if err != nil {
			return RecoveryPlan{}, newError(CodeRecoveryUnavailable, err)
		}
	}
	backupPath := filepath.Join(
		entry.Root, RecoveryDirectoryName, startedAt.Format(recoveryTimestamp),
	)
	if _, statErr := os.Lstat(backupPath); statErr == nil {
		return RecoveryPlan{}, newError(CodeRecoveryIncomplete, errors.New("backup timestamp already exists"))
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return RecoveryPlan{}, newError(CodeRecoveryIncomplete, statErr)
	}
	return RecoveryPlan{
		ProfileKey: entry.Key, ProfileID: profileID, BackupPath: backupPath, StartedAt: startedAt,
		OriginalCode: code,
	}, nil
}

// Recreate preserves the old database set and installs one current pristine database.
func (catalog *Catalog) Recreate(ctx context.Context, request RecoveryRequest) (RecoveryResult, error) {
	if !request.Confirmed {
		return RecoveryResult{}, newError(CodeRecoveryUnavailable, errors.New("recovery was not confirmed"))
	}
	selector := request.Plan.ProfileKey
	if selector == "" {
		selector = request.Plan.ProfileID
	}
	entry, err := catalog.recoveryEntry(ctx, selector)
	if err != nil {
		return RecoveryResult{}, err
	}
	if entry.Status == StatusRequiresNewer || entry.Status == StatusManifestUnsupported {
		return RecoveryResult{}, newError(CodeRecoveryUnavailable, errors.New("profile is not recoverable"))
	}
	profileLock, err := home.TryLock(entry.Root, home.LockProfile, home.LockExclusive)
	if err != nil {
		return RecoveryResult{}, catalogLockError(err)
	}
	defer func() { _ = profileLock.Release() }()

	plan, found, err := scanActiveRecovery(entry.Root, entry.Key)
	if err != nil {
		return RecoveryResult{}, err
	}
	if !found {
		code, codeErr := recoverableOriginalCode(ctx, entry.ProfilePaths())
		if codeErr != nil {
			return RecoveryResult{}, codeErr
		}
		if err = validateRecoveryRequest(entry, request.Plan, code); err != nil {
			return RecoveryResult{}, err
		}
		plan = request.Plan
		if err = catalog.beginRecovery(ctx, entry, plan); err != nil {
			return RecoveryResult{}, err
		}
	} else if request.Plan.ProfileID != plan.ProfileID ||
		(request.Plan.BackupPath != "" && request.Plan.BackupPath != plan.BackupPath) {
		return RecoveryResult{}, newError(CodeRecoveryIncomplete, errors.New("active recovery plan changed"))
	}

	keepMarker := entry.Key == LegacyKey
	if err = catalog.rollForwardRecovery(ctx, entry, plan, keepMarker); err != nil {
		return RecoveryResult{}, err
	}
	if keepMarker {
		if err = profileLock.Release(); err != nil {
			return RecoveryResult{}, newError(CodeRecoveryIncomplete, err)
		}
		profileLock = nil
		providerKind := entry.ProviderKind
		if providerKind == "" {
			providerKind = "local"
		}
		if _, err = catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
			DisplayName: "Moneyflow", ProviderKind: providerKind, ProfileID: plan.ProfileID,
		}); err != nil {
			return RecoveryResult{}, err
		}
		profileLock, err = home.TryLock(entry.Root, home.LockProfile, home.LockExclusive)
		if err != nil {
			return RecoveryResult{}, catalogLockError(err)
		}
		if err = catalog.finishRecovery(plan); err != nil {
			return RecoveryResult{}, err
		}
	}
	return RecoveryResult{BackupPath: plan.BackupPath}, nil
}

func (catalog *Catalog) beginRecovery(
	ctx context.Context,
	entry Entry,
	plan RecoveryPlan,
) error {
	checkpointErr := sqlite.CheckpointProfile(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if checkpointErr != nil && plan.OriginalCode != store.CodeStoreCorrupt {
		return checkpointErr
	}
	recoveryRoot, err := home.EnsurePrivateSubdirectory(entry.Root, RecoveryDirectoryName)
	if err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	backupName := plan.StartedAt.Format(recoveryTimestamp)
	if filepath.Join(recoveryRoot, backupName) != plan.BackupPath {
		return newError(CodeRecoveryIncomplete, errors.New("backup destination changed"))
	}
	if _, err = os.Lstat(plan.BackupPath); err == nil {
		return newError(CodeRecoveryIncomplete, errors.New("backup destination exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return newError(CodeRecoveryIncomplete, err)
	}
	if _, err = home.EnsurePrivateSubdirectory(recoveryRoot, backupName); err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	marker := recoveryMarker{
		MarkerVersion: RecoveryMarkerVersion, ProfileID: plan.ProfileID,
		StartedAt: plan.StartedAt, CreatedByVersion: catalog.version,
		OriginalCode: plan.OriginalCode,
	}
	if err = writeRecoveryMarker(filepath.Join(plan.BackupPath, RecoveryMarkerFilename), marker); err != nil {
		return err
	}
	if err = home.SyncPrivateDirectory(recoveryRoot); err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	return catalog.recoveryFaultAt(RecoveryAfterMarkerWrite)
}

func (catalog *Catalog) rollForwardRecovery(
	ctx context.Context,
	entry Entry,
	plan RecoveryPlan,
	keepMarker bool,
) error {
	state, err := inspectRecoveryState(ctx, entry.ProfilePaths(), plan.BackupPath)
	if err != nil {
		return err
	}
	switch recoveryAction(state) {
	case recoveryActionMoveOld:
		for _, move := range []struct {
			name  string
			point RecoveryFaultPoint
		}{
			{"moneyflow.db-wal", RecoveryAfterWALMove},
			{"moneyflow.db-shm", RecoveryAfterSHMMove},
			{"moneyflow.db", RecoveryAfterMainMove},
		} {
			if err = moveRecoveryFile(entry.Root, plan.BackupPath, move.name, move.name == "moneyflow.db"); err != nil {
				return err
			}
			if err = catalog.recoveryFaultAt(move.point); err != nil {
				return err
			}
		}
		return catalog.installRecoveredProfile(ctx, entry, plan, keepMarker)
	case recoveryActionInstall:
		return catalog.installRecoveredProfile(ctx, entry, plan, keepMarker)
	case recoveryActionFinish:
		if keepMarker {
			return nil
		}
		return catalog.finishRecovery(plan)
	default:
		return newError(CodeRecoveryIncomplete, errors.New("recovery file state is ambiguous"))
	}
}

func (catalog *Catalog) installRecoveredProfile(
	ctx context.Context,
	entry Entry,
	plan RecoveryPlan,
	keepMarker bool,
) error {
	info, err := os.Lstat(entry.ProfilePaths().Database)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err = home.WritePrivateFile(entry.ProfilePaths().Database, nil); err != nil {
			return newError(CodeRecoveryIncomplete, err)
		}
	case err != nil:
		return newError(CodeRecoveryIncomplete, err)
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != 0:
		return newError(CodeRecoveryIncomplete, errors.New("replacement database is not empty"))
	}
	if err = catalog.recoveryFaultAt(RecoveryAfterEmptyCreate); err != nil {
		return err
	}
	if err = sqlite.InstallPristineProfile(ctx, entry.ProfilePaths(), sqlite.DefaultOptions); err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	if err = catalog.recoveryFaultAt(RecoveryAfterSchemaInstall); err != nil {
		return err
	}
	inspection, err := sqlite.InspectProfile(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if err != nil || inspection.Schema != sqlite.SchemaCurrent || !inspection.Pristine {
		return newError(CodeRecoveryIncomplete, errors.New("replacement profile did not verify"))
	}
	if err = catalog.recoveryFaultAt(RecoveryAfterVerification); err != nil {
		return err
	}
	if keepMarker {
		return nil
	}
	return catalog.finishRecovery(plan)
}

func (catalog *Catalog) finishRecovery(plan RecoveryPlan) error {
	markerPath := filepath.Join(plan.BackupPath, RecoveryMarkerFilename)
	if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return newError(CodeRecoveryIncomplete, err)
	}
	if err := home.SyncPrivateDirectory(plan.BackupPath); err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	return catalog.recoveryFaultAt(RecoveryAfterMarkerRemoval)
}

func (catalog *Catalog) recoveryFaultAt(point RecoveryFaultPoint) error {
	if catalog.recoveryFault == nil {
		return nil
	}
	return catalog.recoveryFault(point)
}

func (catalog *Catalog) recoveryEntry(ctx context.Context, selector string) (Entry, error) {
	entries, err := catalog.List(ctx)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.Key == selector {
			return entry, nil
		}
	}
	return Entry{}, newError(CodeProfileNotFound, errors.New("recovery profile is absent"))
}

func validateRecoveryRequest(entry Entry, plan RecoveryPlan, code store.ErrorCode) error {
	if plan.ProfileKey != entry.Key || !ValidProfileID(plan.ProfileID) ||
		(entry.ID != "" && plan.ProfileID != entry.ID) ||
		plan.InProgress || plan.OriginalCode != code ||
		plan.StartedAt.IsZero() || plan.StartedAt.Location() != time.UTC ||
		plan.StartedAt.Format(recoveryTimestamp) != filepath.Base(plan.BackupPath) ||
		filepath.Dir(plan.BackupPath) != filepath.Join(entry.Root, RecoveryDirectoryName) {
		return newError(CodeRecoveryUnavailable, errors.New("recovery confirmation does not match"))
	}
	return nil
}

func recoverableOriginalCode(ctx context.Context, paths home.Paths) (store.ErrorCode, error) {
	original, _, err := inspectRecoveryOriginal(ctx, paths)
	if err != nil {
		return "", err
	}
	switch original {
	case recoveryOriginalOlder:
		return store.CodeSchemaIncompatible, nil
	case recoveryOriginalCorrupt:
		return store.CodeStoreCorrupt, nil
	default:
		return "", newError(CodeRecoveryUnavailable, errors.New("profile condition is not recoverable"))
	}
}

func inspectRecoveryState(
	ctx context.Context,
	paths home.Paths,
	backupPath string,
) (recoveryState, error) {
	backupMain, err := regularFilePresence(filepath.Join(backupPath, "moneyflow.db"))
	if err != nil {
		return recoveryState{}, newError(CodeRecoveryIncomplete, err)
	}
	original, originalMain, err := inspectRecoveryOriginal(ctx, paths)
	if err != nil {
		return recoveryState{}, err
	}
	return recoveryState{
		backupMain: backupMain, originalMain: originalMain, original: original,
	}, nil
}

func inspectRecoveryOriginal(
	ctx context.Context,
	paths home.Paths,
) (recoveryOriginal, bool, error) {
	info, err := os.Lstat(paths.Database)
	if errors.Is(err, os.ErrNotExist) {
		return recoveryOriginalMissing, false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return recoveryOriginalCorrupt, true, nil
	}
	if info.Size() == 0 {
		return recoveryOriginalEmpty, true, nil
	}
	inspection, err := sqlite.InspectProfile(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		var failure *store.Error
		if errors.As(err, &failure) && failure.Code == store.CodeStoreCorrupt {
			return recoveryOriginalCorrupt, true, nil
		}
		return recoveryOriginalCorrupt, true, err
	}
	switch inspection.Schema {
	case sqlite.SchemaOlder:
		return recoveryOriginalOlder, true, nil
	case sqlite.SchemaNewer:
		return recoveryOriginalNewer, true, nil
	case sqlite.SchemaCurrent:
		if inspection.Pristine {
			return recoveryOriginalCurrentPristine, true, nil
		}
		return recoveryOriginalCurrentPopulated, true, nil
	default:
		return recoveryOriginalEmpty, true, nil
	}
}

func recoveryAction(state recoveryState) recoveryActionKind {
	if !state.backupMain {
		if state.originalMain && state.original != recoveryOriginalNewer {
			return recoveryActionMoveOld
		}
		return recoveryActionRefuse
	}
	if !state.originalMain || state.original == recoveryOriginalEmpty {
		return recoveryActionInstall
	}
	if state.original == recoveryOriginalCurrentPristine {
		return recoveryActionFinish
	}
	return recoveryActionRefuse
}

func moveRecoveryFile(sourceRoot, backupRoot, name string, required bool) error {
	source := filepath.Join(sourceRoot, name)
	destination := filepath.Join(backupRoot, name)
	sourceExists, err := regularFilePresence(source)
	if err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	destinationExists, err := regularFilePresence(destination)
	if err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	if sourceExists && destinationExists {
		return newError(CodeRecoveryIncomplete, errors.New("recovery file exists at both paths"))
	}
	if sourceExists {
		if err = home.MovePrivatePath(source, destination); err != nil {
			return newError(CodeRecoveryIncomplete, err)
		}
		return nil
	}
	if required && !destinationExists {
		return newError(CodeRecoveryIncomplete, errors.New("required recovery file is absent"))
	}
	return nil
}

func regularFilePresence(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("recovery path is redirected or not regular")
	}
	return true, nil
}

func scanActiveRecovery(root, profileID string) (RecoveryPlan, bool, error) {
	recoveryRoot := filepath.Join(root, RecoveryDirectoryName)
	info, err := os.Lstat(recoveryRoot)
	if errors.Is(err, os.ErrNotExist) {
		return RecoveryPlan{}, false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return RecoveryPlan{}, false, newError(CodeRecoveryIncomplete, errors.New("recovery root is redirected"))
	}
	children, err := os.ReadDir(recoveryRoot)
	if err != nil {
		return RecoveryPlan{}, false, newError(CodeRecoveryIncomplete, err)
	}
	var plans []RecoveryPlan
	for _, child := range children {
		if child.Type()&os.ModeSymlink != 0 {
			return RecoveryPlan{}, false, newError(CodeRecoveryIncomplete, errors.New("recovery directory is redirected"))
		}
		if !child.IsDir() {
			continue
		}
		backupPath := filepath.Join(recoveryRoot, child.Name())
		markerPath := filepath.Join(backupPath, RecoveryMarkerFilename)
		markerInfo, statErr := os.Lstat(markerPath)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || markerInfo.Mode()&os.ModeSymlink != 0 || !markerInfo.Mode().IsRegular() {
			return RecoveryPlan{}, false, newError(CodeRecoveryIncomplete, errors.New("recovery marker is redirected"))
		}
		marker, readErr := readRecoveryMarker(markerPath)
		if readErr != nil || (profileID != LegacyKey && marker.ProfileID != profileID) ||
			marker.StartedAt.Format(recoveryTimestamp) != child.Name() {
			return RecoveryPlan{}, false, newError(CodeRecoveryIncomplete, errors.New("recovery marker is invalid"))
		}
		plans = append(plans, RecoveryPlan{
			ProfileKey: profileID, ProfileID: marker.ProfileID,
			BackupPath: backupPath, StartedAt: marker.StartedAt,
			OriginalCode: marker.OriginalCode, InProgress: true,
		})
	}
	if len(plans) > 1 {
		return RecoveryPlan{}, false, newError(CodeRecoveryIncomplete, errors.New("multiple recoveries are active"))
	}
	if len(plans) == 0 {
		return RecoveryPlan{}, false, nil
	}
	return plans[0], true, nil
}

func writeRecoveryMarker(path string, marker recoveryMarker) error {
	if err := validateRecoveryMarker(path, marker); err != nil {
		return err
	}
	document := recoveryMarkerDocument{
		MarkerVersion: marker.MarkerVersion, ProfileID: marker.ProfileID,
		StartedAt:          marker.StartedAt.Format(canonicalManifestTime),
		ApplicationVersion: marker.CreatedByVersion, OriginalCode: marker.OriginalCode,
	}
	contents, err := json.Marshal(document)
	if err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	contents = append(contents, '\n')
	if int64(len(contents)) > ManifestMaximumBytes {
		return newError(CodeRecoveryIncomplete, errors.New("recovery marker exceeds maximum size"))
	}
	if err = home.WritePrivateFile(path, contents); err != nil {
		return newError(CodeRecoveryIncomplete, err)
	}
	return nil
}

func readRecoveryMarker(path string) (recoveryMarker, error) {
	fields, err := readManifestObject(path)
	if err != nil {
		return recoveryMarker{}, newError(CodeRecoveryIncomplete, err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, recoveryMarkerFields) {
		return recoveryMarker{}, newError(CodeRecoveryIncomplete, errors.New("recovery marker field set is invalid"))
	}
	var document recoveryMarkerDocument
	for field, target := range map[string]any{
		"marker_version":      &document.MarkerVersion,
		"profile_id":          &document.ProfileID,
		"started_at":          &document.StartedAt,
		"application_version": &document.ApplicationVersion,
		"original_store_code": &document.OriginalCode,
	} {
		if err = json.Unmarshal(fields[field], target); err != nil {
			return recoveryMarker{}, newError(CodeRecoveryIncomplete, fmt.Errorf("decode %s", field))
		}
	}
	startedAt, err := time.Parse(canonicalManifestTime, document.StartedAt)
	if err != nil || startedAt.Format(canonicalManifestTime) != document.StartedAt {
		return recoveryMarker{}, newError(CodeRecoveryIncomplete, errors.New("recovery marker time is invalid"))
	}
	marker := recoveryMarker{
		MarkerVersion: document.MarkerVersion, ProfileID: document.ProfileID,
		StartedAt: startedAt, CreatedByVersion: document.ApplicationVersion,
		OriginalCode: document.OriginalCode,
	}
	if err = validateRecoveryMarker(path, marker); err != nil {
		return recoveryMarker{}, err
	}
	return marker, nil
}

func validateRecoveryMarker(path string, marker recoveryMarker) error {
	if marker.MarkerVersion != RecoveryMarkerVersion ||
		!ValidProfileID(marker.ProfileID) ||
		marker.StartedAt.IsZero() || marker.StartedAt.Location() != time.UTC ||
		marker.StartedAt.Format(recoveryTimestamp) != filepath.Base(filepath.Dir(path)) ||
		validateVersionString(marker.CreatedByVersion) != nil ||
		!recoverableStoreCode(marker.OriginalCode) {
		return newError(CodeRecoveryIncomplete, errors.New("recovery marker is invalid"))
	}
	return nil
}

func recoverableStoreCode(code store.ErrorCode) bool {
	return code == store.CodeSchemaIncompatible || code == store.CodeStoreCorrupt ||
		code == store.ErrorCode("journal_payload_incompatible")
}
