package moneyflow

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// backupFiles are the only files a backup profile directory holds.
var backupFiles = map[string]bool{
	profilecatalog.ManifestFilename: true,
	"moneyflow.db":                  true,
	"audit.jsonl":                   true,
	"provider-refresh.json":         true,
}

// Backup writes a data directory at the absolute path destination, which must not exist. It
// holds each profile's profile.json, a consolidated moneyflow.db, audit.jsonl and
// provider-refresh.json, and no provider credentials, sessions or exports. It copies audit.jsonl
// only through its last newline, so it leaves out a final record that a provider write is still
// appending or that a crash left incomplete. A refresh that runs during the backup can leave
// provider-refresh.json older than moneyflow.db, never newer, so a restored profile may read
// complete history sooner than it needs to but never later. Backup skips a profile it cannot open
// or copy, copies the others and then returns an error that names each skipped profile; Verify
// passes on that destination. Any other failure leaves no destination. It returns ErrClosed after
// Close. If ctx ends or the instance closes during the backup, it returns ctx's error or
// ErrClosed, not an error that blames the profiles it could not copy.
//
// Backup creates destination in one step that fails when anything exists there, and it removes
// only a destination it created. When another backup or process creates destination first, the
// call fails and leaves that path as it was. Backup creates missing parent directories and leaves
// an existing parent's permissions alone; it restricts only destination to its owner.
func (instance *Instance) Backup(ctx context.Context, destination string) error {
	if err := instance.checkOpen(); err != nil {
		return err
	}
	if !filepath.IsAbs(destination) {
		return errors.New("back up profiles: the destination must be an absolute path")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return backupDestinationExists(destination)
	}
	entries, err := instance.list(ctx)
	if err != nil {
		return fmt.Errorf("back up profiles: %w", err)
	}
	if err = reserveBackupDestination(destination); err != nil {
		return err
	}
	if err = instance.backUpProfiles(ctx, entries, destination); err != nil {
		return fmt.Errorf("back up profiles: %w", err)
	}
	return nil
}

// reserveBackupDestination creates destination, which makes the calling backup its only owner. It
// fails without touching destination when anything exists there, including a path that appeared
// after the caller checked.
func reserveBackupDestination(destination string) error {
	err := home.CreatePrivateRoot(destination)
	if errors.Is(err, home.ErrPrivateDestinationExists) {
		return backupDestinationExists(destination)
	}
	if err != nil {
		return fmt.Errorf("back up profiles: %w", err)
	}
	return nil
}

func backupDestinationExists(destination string) error {
	return fmt.Errorf("back up profiles: %s already exists", destination)
}

// backUpProfiles copies each profile into destination's profiles directory and returns an error
// that names each profile it skipped. Once ctx ends or the instance closes, it stops, removes
// destination and returns ctx's error or ErrClosed.
func (instance *Instance) backUpProfiles(
	ctx context.Context,
	entries []profilecatalog.Entry,
	destination string,
) error {
	if _, err := home.EnsurePrivateSubdirectory(destination, "profiles"); err != nil {
		return errors.Join(err, os.RemoveAll(destination))
	}
	var skipped []error
	for _, entry := range entries {
		err := instance.backUpProfile(ctx, entry, destination)
		// An interruption also fails the copy, through no fault of the profile.
		if interrupted := cmp.Or(ctx.Err(), instance.checkOpen()); interrupted != nil {
			return errors.Join(interrupted, os.RemoveAll(destination))
		}
		if err != nil {
			skipped = append(skipped, fmt.Errorf("skipped profile %s: %w", entry.ID, err))
		}
	}
	return errors.Join(skipped...)
}

// backUpProfile copies one profile while the instance holds it open, so its database has a
// write-ahead log and the copy reads a consistent snapshot. It removes a copy it cannot finish.
func (instance *Instance) backUpProfile(
	ctx context.Context,
	entry profilecatalog.Entry,
	destination string,
) (err error) {
	if problem := entryProblem(entry); problem != "" {
		return errors.New(problem)
	}
	lease, err := instance.registry.Acquire(ctx, entry.ID)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Release() }()
	root, err := home.EnsurePrivateSubdirectory(destination, "profiles", entry.ID)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(root))
		}
	}()
	manifest, err := home.ReadPrivateFile(
		filepath.Join(entry.Root, profilecatalog.ManifestFilename), profilecatalog.ManifestMaximumBytes,
	)
	if err != nil {
		return err
	}
	err = home.WritePrivateFile(filepath.Join(root, profilecatalog.ManifestFilename), manifest)
	if err != nil {
		return err
	}
	return instance.copyBackupFiles(ctx, entry.ProfilePaths(), root)
}

// copyBackupFiles writes a profile's refresh record, a consistent copy of its database and its
// audit log through the last complete record into root. It copies the record first: a refresh
// records a complete-history read only after it commits, so the copied record never claims a read
// that the copied database lacks. A refresh between the two copies leaves the record older than
// the database, which only makes the next complete-history read due sooner. A provider write can
// append to the log during the copy, and moneyflow refuses to append to a log whose final record
// is incomplete.
func (instance *Instance) copyBackupFiles(
	ctx context.Context,
	source home.Paths,
	root string,
) error {
	const refresh, audit = "provider-refresh.json", "audit.jsonl"
	err := copyPrivateFile(filepath.Join(source.Root, refresh), filepath.Join(root, refresh))
	if err != nil {
		return err
	}
	if instance.seams.betweenBackupCopies != nil {
		instance.seams.betweenBackupCopies()
	}
	destination := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	if err = sqlite.CopyProfile(ctx, source, destination, instance.sqlite); err != nil {
		return err
	}
	return copyCompleteRecords(filepath.Join(source.Root, audit), filepath.Join(root, audit))
}

// copyCompleteRecords copies the owner-only JSON Lines file source through its last newline,
// leaving out a final record without one. A missing source is not an error.
func copyCompleteRecords(source, destination string) error {
	file, err := home.OpenPrivateFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return home.WritePrivateNoReplace(destination, func(writer io.Writer) error {
		reader, buffered := bufio.NewReader(file), bufio.NewWriter(writer)
		for {
			record, readErr := reader.ReadBytes('\n')
			if errors.Is(readErr, io.EOF) {
				return buffered.Flush()
			}
			if readErr != nil {
				return readErr
			}
			if _, writeErr := buffered.Write(record); writeErr != nil {
				return writeErr
			}
		}
	})
}

// Verify checks that root holds a backup in the layout Backup writes: a profiles directory that
// holds profile directories named by ID, each with a matching profile.json and a sound
// current-schema database, and nothing else. It rejects a symbolic link where Backup writes a
// directory, which moneyflow would refuse to open. It rejects an audit.jsonl whose final record
// is incomplete: one that is not empty and does not end in a newline. It takes no locks, so verify
// a backup nothing else is using. It changes no file's contents, but like any moneyflow open it
// may restrict a looser file to owner-only access; on Windows it resets the access list of each
// file it reads.
func Verify(ctx context.Context, root string) error {
	if !filepath.IsAbs(root) {
		return errors.New("verify backup: root must be an absolute path")
	}
	if err := verifyRootEntries(root); err != nil {
		return fmt.Errorf("verify backup: %w", err)
	}
	profiles, err := readProfileDirectories(root)
	if err != nil {
		return fmt.Errorf("verify backup: %w", err)
	}
	for _, profile := range profiles {
		if err = verifyProfile(ctx, filepath.Join(root, "profiles", profile.Name())); err != nil {
			return fmt.Errorf("verify backup: profile %s: %w", profile.Name(), err)
		}
	}
	return nil
}

// verifyRootEntries checks that the only entry in root is a profiles directory. It reads no
// profile and follows no link: a directory listing reports each entry's own type.
func verifyRootEntries(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "profiles" {
			return fmt.Errorf("%s does not belong in a backup", entry.Name())
		}
		if err = requireDirectory(entry, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// readProfileDirectories lists root's profiles directory and checks, before any profile is read,
// that each entry in it is a directory and not a link.
func readProfileDirectories(root string) ([]os.DirEntry, error) {
	profiles, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if err = requireDirectory(profile, filepath.Join("profiles", profile.Name())); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

// requireDirectory fails for an entry, at path in the backup, that is a symbolic link or is not a
// directory. A link to a directory is still a link, because a listing reports the entry's own type.
func requireDirectory(entry os.DirEntry, path string) error {
	switch {
	case entry.Type()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symbolic link", path)
	case !entry.IsDir():
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func verifyProfile(ctx context.Context, root string) error {
	if !profilecatalog.ValidProfileID(filepath.Base(root)) {
		return errors.New("the directory name is not a profile ID")
	}
	if err := verifyFiles(root); err != nil {
		return err
	}
	// ReadManifest also checks that the manifest's profile ID is the directory's name.
	_, err := profilecatalog.ReadManifest(filepath.Join(root, profilecatalog.ManifestFilename))
	if err != nil {
		return err
	}
	paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	database, err := sqlite.OpenTransferSource(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		return err
	}
	return database.Close()
}

// verifyFiles checks that the profile directory root holds only backup files and that its audit
// log ends in a complete record.
func verifyFiles(root string) error {
	files, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !backupFiles[file.Name()] || !file.Type().IsRegular() {
			return fmt.Errorf("%s does not belong in a backup", file.Name())
		}
	}
	return verifyCompleteRecords(filepath.Join(root, "audit.jsonl"))
}

// verifyCompleteRecords checks that the JSON Lines file at path is missing, empty or ends in a
// newline.
func verifyCompleteRecords(path string) error {
	file, err := home.OpenPrivateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	}
	var last [1]byte
	if _, err = file.ReadAt(last[:], info.Size()-1); err != nil {
		return err
	}
	if last[0] != '\n' {
		return fmt.Errorf("%s ends in an incomplete record", filepath.Base(path))
	}
	return nil
}
