# Go Port Profile Catalog and Onboarding Design

**Status:** Approved

## Purpose

Moneyflow v2 must complete the profile-selection and provider-onboarding experience that already
exists in Python. A user starting either interactive Go interface must be able to choose a profile,
create a Monarch profile, protect credentials with a Moneyflow account password, watch connection
and import progress, recover an incompatible preview profile, and enter the finance application
without leaving that interface.

The supported primary entry points are:

```text
moneyflow tui
moneyflow web
```

`moneyflow provider connect monarch` remains an advanced headless and recovery command. It is not a
separate implementation: Cobra, Bubble Tea, and the browser present the same application-layer
catalog and onboarding workflow.

This design deliberately supersedes the earlier SQLite slice's deferral of multi-profile
management and the Monarch read/refresh slice's CLI-only connection constraint. It does not
supersede their accounting, journal, provider-refresh, security, or install-only schema contracts.

## Goals

This slice provides:

- a filesystem-discovered, multi-profile catalog with stable opaque identities;
- selector-first startup for TUI and web;
- one renderer-neutral Monarch connection state machine;
- password-protected credential setup and unlock in all three presenters;
- visible authentication and import progress;
- safe, crash-resumable recreation of incompatible preview profiles;
- profile-scoped browser routes and APIs; and
- an in-place transition for the existing root-level Go preview profile.

The workflow follows the Python `AccountFlowCoordinator`, `AccountSelectorScreen`,
`CredentialSetupScreen`, and `CredentialUnlockScreen` where their behavior remains applicable.
The Go design improves on the Python experience with explicit phases, counts-only progress,
resumable retained sessions, multi-process safety, and stable profile URLs.

## Non-Goals

This slice does not add:

- SQLite or journal-payload migrations;
- provider write-back;
- YNAB or SimpleFIN adapters;
- general profile rename, delete, import, or export;
- last-used ordering;
- a central profile-registry database or `accounts.json` equivalent;
- automatic destructive recovery for a profile created by a newer Moneyflow version; or
- changes to the Python runtime.

YNAB and SimpleFIN remain visible but disabled in provider selection. Selecting either shows that
the provider is not available in Go yet.

## Chosen Architecture

Moneyflow uses one shared profile/onboarding application service with renderer-specific
presenters. The process starts profile-neutral unless `--demo` or `--profile` selects a profile
explicitly.

The dependency directions are:

```text
cmd/moneyflow ─┬─> internal/onboarding ─> internal/profilecatalog ─┬─> internal/home
               │            │                                     └─> internal/store/sqlite
               │            ├─> internal/app
               │            └─> internal/provider/monarch
               ├─> internal/tui
               └─> internal/web + internal/api

internal/profilecatalog -X-> internal/provider/**
internal/store/**         -X-> internal/provider/**
internal/provider/**      -X-> internal/store/**
```

`internal/home` owns private paths, hardened file operations, and cross-platform advisory locks.
`internal/profilecatalog` owns discovery, manifest validation, name resolution, profile creation,
cancel cleanup, local status inspection, and recovery planning. `internal/onboarding` owns the
connection state machine and composes catalog handles, the existing application service, and the
Monarch runtime interfaces. Command, TUI, API, and web packages are presenters.

Catalog code never imports a provider package. Only onboarding and the command's factory wiring may
import `internal/provider/monarch` in this slice. Provider implementations remain unaware of
SQLite, catalogs, and renderers. Profile catalog calls narrow exported SQLite helpers for shallow
schema inspection, checkpointing, and current-schema installation; it never opens a driver
connection or handles SQL rows itself. SQL rows and driver types remain confined to the store.

## Catalog Root and Profile Layout

`MONEYFLOW_HOME` and every explicit home option now identify the catalog root. With no override,
the catalog root remains:

```text
~/.moneyflow/v2/
```

New profiles use this layout:

```text
~/.moneyflow/v2/
├── catalog.lock
└── profiles/
    └── profile_<26 lowercase base32 characters>/
        ├── profile.json
        ├── profile.lock
        ├── provider-connect.lock
        ├── moneyflow.db
        ├── providers/
        │   └── monarch/
        │       ├── credentials.enc
        │       └── session.json
        └── recovery/
            └── 20260817T191234.123456789Z/
                ├── recovery-in-progress.json
                ├── moneyflow.db
                ├── moneyflow.db-wal
                └── moneyflow.db-shm
```

The exact provider filenames continue to be owned by the Monarch package; the names above show
their conceptual placement rather than moving their schema into the catalog.

The existing preview layout remains valid in place:

```text
~/.moneyflow/v2/
├── profile.json
├── profile.lock
├── provider-connect.lock
├── moneyflow.db
├── providers/
└── recovery/
```

The profile abstraction always returns a profile root. The legacy profile's root happens to be the
catalog root; downstream opening, credential, locking, and recovery code does not otherwise branch
on the legacy layout.

### Profile IDs

A profile ID is 128 bits from `crypto/rand`, encoded with unpadded lowercase base32 and prefixed by
`profile_`. The encoded random portion is exactly 26 characters. Generation follows the injected
random-reader pattern used by `domain.NewOperationID` and refuses short reads. The complete ID is
the profile directory name and the stable browser-route key.

Profile IDs are never reused. The catalog rejects directories and manifests whose IDs are not in
canonical form or whose manifest ID does not equal the directory name. The legacy profile receives
a generated ID in its manifest; until that manifest can be written, discovery exposes one
process-stable synthetic legacy key that is never accepted as a persisted profile ID or bookmark.

### Profile Manifest

The manifest filename is `profile.json`. Manifest version one contains exactly:

```json
{
  "manifest_version": 1,
  "profile_id": "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
  "display_name": "Moneyflow",
  "provider_kind": "monarch",
  "created_at": "2026-08-17T19:12:34.123456789Z",
  "created_by_version": "0.12.0"
}
```

The fields have these rules:

- `manifest_version` is the integer `1`. It is independent of SQLite's
  `CurrentSchemaVersion`.
- `profile_id` uses the canonical profile-ID format and matches its directory, except that the
  legacy root has no directory-name comparison.
- `display_name` is trimmed and control-free, contains 1 through 80 Unicode code points, and is at
  most 320 UTF-8 bytes.
- `provider_kind` is `monarch` or `local` in this slice.
- `created_at` is canonical UTC RFC 3339 with nanoseconds.
- `created_by_version` is the exact application version string at creation and is informational.

The decoder rejects missing, duplicate, and unknown fields, trailing JSON, oversized files, and
noncanonical values. An unknown `manifest_version` produces `profile_manifest_unsupported`; the
profile is listed as unsupported but cannot be opened, rewritten, or recovered by this binary.
Moneyflow never guesses whether an unknown manifest is older or newer.

Manifests are written with the existing owner-private atomic replacement discipline. Display-name
uniqueness uses the same NFKC, case-fold, and whitespace collision-key behavior as domain labels.
Catalog listing sorts display names by that normalized key, then profile ID. There is no last-used
field or open-time catalog write.

### Legacy Discovery

The catalog discovers the legacy root from the presence of root-level `moneyflow.db`, regardless
of schema compatibility and regardless of whether `profile.json` exists. Without a manifest it is
listed as `Moneyflow`. A compatible successful open writes the version-one manifest in place after
opening the database. For that one manifest-less legacy open, the caller takes `catalog.lock`
exclusively before the shared `profile.lock`, includes the synthetic `Moneyflow` entry in the
display-name conflict check, writes the manifest, and then releases the locks. It never attempts a
catalog acquisition from an already-open ordinary profile.

An incompatible legacy database therefore remains visible and can enter recovery even though it
could never have produced a manifest. A newer database is also visible but is never recoverable by
this binary.

## Local Profile Status

Catalog listing performs no provider network I/O. Status comes only from the manifest, shallow
SQLite inspection, recovery markers, provider binding, and session-file presence:

- `ready`: bound profile with a locally present session file;
- `reconnect`: bound profile without a session file;
- `setup_incomplete`: unbound pristine profile;
- `local_only`: unbound non-pristine profile;
- `needs_recovery`: older/incompatible schema, supported journal-payload incompatibility, corrupt
  store discovered locally, or an incomplete supported recovery;
- `requires_newer_moneyflow`: `schema_newer`; and
- `manifest_unsupported`: unknown manifest version.

Shallow listing never loads all transactions and never performs a full provider refresh. If a
deeper corruption or expired session is discovered only after selection, the presenter updates the
profile's in-memory status and routes to the corresponding recovery or reconnect screen.

`ready` means only that local session material exists. Session expiry is learned when opening or
refreshing the selected profile.

## Catalog and Lifecycle Locks

All lock files are OS advisory locks, not ownership marker files. Process death releases them.
They use the portable, identity-safe locking discipline already proven in `docbank`; filesystem
paths and ancestors are validated before acquisition.

The fixed acquisition order is:

```text
catalog.lock -> profile.lock -> provider-connect.lock
```

Code never acquires an earlier lock while holding a later lock. Catalog-only listing takes no
profile lock beyond a bounded shared inspection. Profile creation takes the catalog lock and then
the new profile's exclusive lifecycle lock. An ordinary open holds a shared `profile.lock` for the
lifetime of its service. Recovery takes only an exclusive `profile.lock`. If a manifest must be
written afterward, recovery first releases that lock; a separate operation then acquires
`catalog.lock` followed by `profile.lock` and revalidates the profile before writing.

The provider-connect lock is exclusive per profile for the duration of one connection attempt. A
second process reports `provider_connect_in_progress`. Existing SQLite refresh leases still
coordinate remote snapshot fetch/fold behavior; neither advisory lock replaces revision or refresh
generation checks.

For web recovery, the server first evicts and closes its own cached service for the profile,
releasing its shared lifecycle lock. It then attempts the exclusive lock. This avoids self-conflict
under `flock` and `LockFileEx` semantics.

## Profile Creation and Cancel Cleanup

Add Profile follows the Python order:

```text
provider -> display name -> durable empty profile -> connection workflow
```

Accepting the display name creates the private directory, manifest, and exact current empty SQLite
schema. Cancel follows Python while preserving the newer retained-session contract:

- if the profile remains pristine and has no session or credential vault, cancellation removes the
  exact newly created, catalog-owned profile directory;
- if authentication has saved a session or vault, cancellation retains the profile as
  `setup_incomplete`; and
- a crash or uncertain cleanup state retains the profile and never guesses that removal is safe.

This narrow rollback is part of Add Profile. It does not provide general profile deletion.

Demo profiles remain fresh, temporary, synthetic, unregistered, and cleaned by the existing
owned-temporary-root discipline. `--fixture` continues to imply a temporary profile.

## Recovery Contract

Recreate is offered only for `schema_incompatible`, supported journal-payload incompatibility, and
`store_corrupt`. `schema_newer` shows `Requires a newer Moneyflow` and never exposes Recreate.
Unknown manifest versions are likewise non-destructive refusals.

The recovery confirmation states that Moneyflow will preserve the old database files, install a
pristine current schema, and then return to ordinary connection setup. It displays the eventual
backup path. No recovery starts without explicit confirmation.

### Recovery Directory and Marker

The backup is a subdirectory of the profile root so every move stays on one filesystem. Its name is
the injected current UTC time formatted as `20060102T150405.000000000Z`. An existing same-name
directory is a refusal rather than an overwrite.

Before moving any database file, Moneyflow creates the backup directory and atomically writes
`recovery-in-progress.json` inside it. Marker version one contains exactly the profile ID, UTC start
time, application version, and the original storage error code. It contains no display name or
financial data.

Catalog discovery scans the fixed `recovery/` directory for a valid in-progress marker. Invalid,
redirected, oversized, or multiple active markers produce `profile_recovery_incomplete` and require
manual inspection; Moneyflow does not choose among them.

### Recovery Procedure

Under the exclusive lifecycle lock, recovery:

1. opens a bounded raw SQLite connection and attempts `wal_checkpoint(TRUNCATE)` before closing it;
2. creates and fsyncs the backup directory and in-progress marker;
3. moves `moneyflow.db-wal` and `moneyflow.db-shm`, when present, into the backup;
4. moves `moneyflow.db` into the backup;
5. creates and validates the exact current empty schema at the original database path; and
6. removes the in-progress marker only after the new database is verified current and pristine.

If the old database is corrupt enough that checkpointing cannot succeed, explicit corrupt-store
recovery may preserve and move the main/WAL/SHM set without checkpointing. It never discards a
member of the set. Any failure before the first move leaves the complete logical database at its
original path.

The session and encrypted credential vault are outside the moved database set and survive. The
provider binding is intentionally lost with the old database. Recovery itself ends at a pristine
profile and releases the exclusive lifecycle lock. The presenter then enters the same ordinary
unbound-profile connection workflow used everywhere else.

### Idempotent Roll-Forward

The marker and actual files, not a mutable stage counter, are authoritative. Startup/listing routes
an incomplete recovery through these idempotent cases:

- if the marker exists and `recovery/<timestamp>/moneyflow.db` does not exist, any main file at the
  original path is the unmoved old database; continue sidecar-before-main moves;
- if the backup main exists and no database exists at the original path, install and verify the
  current schema; and
- if the backup main and an original-path database both exist, verify the original-path database;
  install if it is empty, or clear the marker only if it is already current and pristine.

The presence of the backup main is therefore the exact disambiguator between an old database still
awaiting its move and a newly created database. Repeating any case is safe. When the backup main
exists, a nonempty, noncurrent original-path database is `profile_recovery_incomplete`; Moneyflow
never overwrites it. A completed backup remains after the marker is removed. The UI displays its
exact path, but logs do not contain it.

## Profile Selection Experience

### Terminal Selector

Ordinary `moneyflow tui` starts one top-level Bubble Tea router without opening a profile first.
The account selector mirrors Python's structure:

- alphabetically sorted named profiles with provider and local status;
- Demo;
- Add profile; and
- Exit.

Up/Down, `j`/`k`, Home, Enter, Escape, and `q` work consistently. Python's direct selector keys
`d`, `a`, and `n` remain available. Provider selection keeps `m`, `y`, and `s`. Disabled YNAB and
SimpleFIN rows remain focusable; activation explains that the adapter is unavailable.

Selection behavior is:

- `ready` opens the finance application immediately;
- `reconnect` enters the connection coordinator;
- `setup_incomplete` resumes onboarding;
- `local_only` offers Open Offline or Back;
- `needs_recovery` opens recovery guidance and confirmation;
- `requires_newer_moneyflow` gives non-destructive upgrade guidance; and
- `manifest_unsupported` gives non-destructive manifest-version guidance.

Returning to profile selection closes the TUI's current service and shared lifecycle lock. The
existing finance model remains profile-scoped and does not learn catalog or credential details.

The current `tui.Run(ctx, service, session, ...)` and `IOStreams.RunTUI` seam changes for ordinary
selector-first startup: the top-level model receives injected catalog/opener/onboarding interfaces
and owns opening and closing selected profiles. `--demo` and `--profile` remain preselected paths
that may construct a service before entering the program.

### Browser Selector and Routing

`moneyflow web` starts a profile-neutral server. The configured base path serves one SPA and its
assets. The base route displays the catalog unless a command-line preselection redirects it.

Profile app routes use:

```text
<base-path>p/<profile-id>/?v=1&group=merchant
```

Profile API routes use:

```text
<base-path>api/v1/profiles/<profile-id>/...
```

The existing analytical view remains in the query string and `OwnedHistoryLedger`; the profile ID
is path state. Assets remain at the base path and are not duplicated below each profile. Separate
tabs may use different profiles concurrently. There is no process-global selected profile.

`moneyflow web --demo` and `moneyflow web --profile NAME_OR_ID` use the same routing model: the base
path redirects to the selected `/p/<id>/` route. Demo receives one process-local route ID that is
never written to the catalog. Existing editing E2E journeys are updated to follow the redirect.

The server lazily opens profile services by ID. Services may remain cached while in use and close
after a bounded idle period or at shutdown. URL state, not a cached service, owns the durable
profile/view selection.

The web selector and wizard use `kit-ui`, visible focus, accessible labels and announcements, and
the same keyboard paths as the TUI. Mouse input is optional. Every action is keyboard reachable.

## Renderer-Neutral Connection State Machine

The Cobra-bound orchestration currently in `cmd/moneyflow/provider.go` moves into
`internal/onboarding`. The existing `MonarchCommandRuntime` dependency shape becomes a
renderer-neutral injected runtime: connector, session store, credential vault, source, instance
ID, clock, and randomness. Presenters do not authenticate or persist provider material directly.

The state-machine protocol version is the integer constant `1`. Its named states are:

```text
inspect
validate_session
settings_required
unlock_required
credentials_required
authenticating
importing
complete
local_only
identity_mismatch
failed
canceled
```

Each attempt has a cryptographically random opaque ID bound to the server/process instance and the
profile ID, plus a monotonically increasing `state_version`. Every submit and cancel supplies the
expected state version. Duplicate or stale transitions are rejected without repeating provider or
storage effects.

### Transition Order

The exact order is:

```text
inspect
-> validate retained session
-> settings if import configuration is still absent
-> unlock saved credentials or enter new credentials if authentication is required
-> authenticate
-> import
-> complete
```

The coordinator preserves existing precedence:

1. a committed provider binding supplies currency and scale;
2. otherwise a saved session supplies its import configuration; and
3. otherwise explicit user input supplies the configuration.

A conflicting explicit override is rejected. When no source supplies settings, presenters show
`USD` and scale `2` as visible defaults and require confirmation. Cobra flags remain noninteractive
overrides; without them, Cobra now prompts for the same default confirmation instead of returning
the old `credential setup requires explicit --currency and --scale` error.

`inspect` has three non-progressing outcomes:

- unbound non-pristine profiles enter `local_only` and remain browseable without binding;
- a bound profile paired with a session for a different remote identity enters
  `identity_mismatch`, offers Re-enter credentials or Cancel, and changes no local data; and
- unsupported local schema/manifest state routes back to catalog recovery or upgrade guidance.

### Retained Session and Credential Rules

The coordinator validates a retained session before asking for settings or credentials. A valid
session proceeds even when no credential vault exists. This intentionally improves on the current
Go preview rule requiring both `sessionValid && vaultExists`. A vault becomes necessary only when
reauthentication is required.

If the session is absent or returns `provider_reconnect_required`, the coordinator:

- enters `unlock_required` when a vault exists; or
- enters `credentials_required` when it does not.

New setup collects Monarch email, Monarch password, Base32 TOTP secret, Moneyflow account password,
and account-password confirmation. Unlock collects only the Moneyflow account password. TOTP codes
and MFA responses are generated automatically from the saved Base32 secret, matching Python.

Successful authentication validates remote identity before saving. Session and encrypted
credentials are saved through their existing hardened atomic stores. If the user cancels or the
process crashes after authentication but before import folding, the valid session and pristine
profile remain. The next attempt validates that session and continues directly to import.

An import failure after a valid session offers Retry without repeating credential entry. An import
failure whose code is `provider_reconnect_required` instead returns to session validation and then
unlock/authentication; it is never blindly retried. Identity mismatch never folds or rewrites the
profile.

### Runtime Installation and Reconnect

On successful import, the coordinator installs the configured provider runtime into the already
open application service. TUI and web enter the bound finance view without restarting.

If an already open finance view later receives `provider_reconnect_required`, both renderers show a
Reconnect action in place. It enters the coordinator at `validate_session` for that profile and
returns to the preserved analytical view after success. The user does not have to return to the
selector.

## Secrets and Progress

TUI secret inputs display bullets for every entered character and preserve visible cursor/editing
feedback. Web uses labeled password inputs. After submit, secret fields are cleared from presenter
state and mutable Go byte buffers are cleared promptly.

Secrets never enter URLs, browser history, local/session storage, API responses, progress status,
logs, profile manifests, SQLite, or raw error messages. Only the existing encrypted Monarch vault
persists email, password, and TOTP secret. The account password is never persisted.

Network work is asynchronous for TUI and web. Presenters immediately show one of:

```text
Checking saved session
Authenticating with Monarch
Fetching Monarch data
Verifying Monarch data
Importing Monarch data
```

Progress uses the existing counts-only `provider.Progress` observer. Status includes elapsed time,
partition, fetched count, total count, attempt, and pass where applicable. TUI observes process
state through Bubble Tea commands/ticks. Web starts a bounded process-local job and polls status.
Cancellation cancels network work; an SQLite fold remains atomic.

Wrong account password returns to `unlock_required` with the submitted secret cleared.
Authentication failure returns to `credentials_required` with all submitted secret fields cleared.
Nonsecret settings may remain populated.

## Onboarding HTTP Contract

The browser uses these profile-scoped endpoints:

```text
POST /api/v1/profiles/{profile_id}/onboarding/start
POST /api/v1/profiles/{profile_id}/onboarding/{attempt_id}/submit
POST /api/v1/profiles/{profile_id}/onboarding/{attempt_id}/cancel
GET  /api/v1/profiles/{profile_id}/onboarding/{attempt_id}/status
```

Start, submit, and cancel are mutations protected by the existing mutation-token header, canonical
Origin validation, Fetch Metadata checks, no-store response policy, and bounded body parsing.
Onboarding tokens are additionally bound to the profile ID. Status is same-origin, no-store, and
credential blind. All endpoints use the existing configured base path.

Submit contains `protocol_version`, `expected_state_version`, one action discriminator, and exactly
the bounded payload for that action. Version-one actions are:

```text
confirm_settings
unlock
submit_credentials
retry
reauthenticate
```

The status response contains exactly this logical shape:

```text
protocol_version: 1
attempt_id: opaque string
profile_id: canonical profile ID
state_version: unsigned integer
state: named state
provider_kind: "monarch"
settings: optional { currency: exact string, scale: integer }
progress: optional {
  phase: stable nonsecret string,
  partition: stable nonsecret string,
  fetched: integer,
  total: integer,
  attempt: integer,
  pass: integer,
  elapsed_ms: integer
}
failure: optional {
  code: stable error code,
  message: sanitized user-facing string,
  can_retry: boolean,
  can_reenter: boolean
}
```

No submitted field is copied into status. Huma problem responses never contain request bodies or
raw provider errors. Attempt IDs are random, expire after 30 minutes without activity, and become
invalid on server restart. Activity means a successful status poll, a submitted transition, or an
actively running coordinator job; a long authentication or import job cannot expire its own
attempt. Expiration never rolls back already durable session, vault, or profile effects.

Catalog list/create and recovery endpoints are separate profile-catalog operations under the same
security envelope. They never accept provider credentials.

## Stable Error Codes

The catalog/onboarding layer defines and exhaustively tests these new codes:

```text
profile_not_found
profile_ambiguous
profile_name_conflict
profile_invalid
profile_manifest_unsupported
profile_busy
profile_recovery_incomplete
profile_recovery_unavailable
onboarding_stale
onboarding_expired
onboarding_canceled
onboarding_local_only
credential_unlock_failed
credential_input_invalid
provider_connect_in_progress
```

Existing `schema_newer`, `schema_incompatible`, `store_corrupt`, store concurrency, and provider
codes retain their established meanings. API mapping is exhaustive: unknown internal errors become
one sanitized generic failure, never a raw string.

`profile_recovery_unavailable` covers a destructive recovery request against a newer schema,
unknown manifest, or other explicitly non-recoverable state. `profile_recovery_incomplete` covers
ambiguous or unsafe roll-forward state. `profile_busy` covers lifecycle-lock conflict;
`provider_connect_in_progress` covers only the provider-connect lock.

## Command-Line Contract

The advanced commands become profile-aware:

```text
moneyflow provider connect monarch --profile NAME_OR_ID
moneyflow provider disconnect monarch --profile NAME_OR_ID
moneyflow tui --profile NAME_OR_ID
moneyflow web --profile NAME_OR_ID
```

`--profile` accepts either a canonical profile ID or an exact NFKC/case-folded display-name match.
An ambiguous or missing name fails before opening any profile and prints the available names with
copyable IDs. Omitted `--profile` uses the sole persistent profile when exactly one exists; with
multiple profiles it requires explicit selection for headless provider commands.
Interactive TUI and web continue to show the selector when not preselected.

The Cobra presenter drives the same state machine synchronously. It retains secret terminal echo
suppression, progress on standard error, the one import-summary line on standard output, and the
existing next-step guidance. `--currency`, `--scale`, and `--mtd` remain advanced overrides.

## Logging and Privacy

Persisted and logged diagnostics use an allowlist. They may contain:

- stable error codes;
- opaque profile IDs;
- revision and refresh-generation numbers;
- counts and timings; and
- correlation IDs.

They never contain display names, filesystem paths, email addresses, passwords, TOTP material,
search text, merchant/category/account names, transaction data, request bodies, or raw provider
responses. An interactive recovery screen may display the selected profile's backup path; that
path is not logged or placed in a browser URL.

## Testing Strategy

All implementation follows repository TDD and uses Testify. Tests use explicit temporary catalog
roots and synthetic provider data. No test, demo, screenshot, semantic frame, or log fixture may
contain real financial or credential data.

### Catalog and Home Tests

Tests prove:

- canonical 128-bit profile ID generation and rejection;
- strict manifest decoding, size bounds, version refusal, atomic private writes, and name
  normalization;
- legacy-root discovery with no manifest for current, older, newer, and corrupt schemas;
- alphabetical listing without network I/O or last-used writes;
- exact name/ID resolution and ambiguous-name errors;
- Add cancellation removes only a pristine artifact-free new profile;
- shared/exclusive lifecycle and exclusive provider-connect contention across helper processes;
- catalog/lifecycle/connect acquisition order through architecture and lock-order tests;
- symlink, redirected-root, ownership, and permission defenses on supported platforms; and
- manifest rewrites never require a reverse lock acquisition.

### Recovery Tests

Fault injection stops after every marker, sidecar rename, main rename, schema creation, and marker
removal boundary. Reopening repeatedly proves:

- marker plus original main continues moves;
- marker plus moved original installs the current schema;
- marker plus empty new database installs the schema;
- marker plus current pristine database clears the marker;
- ambiguous/noncurrent new-path data is never overwritten;
- exactly one intact backup remains;
- vault and session files remain unchanged;
- newer schemas and unknown manifests never expose or execute Recreate; and
- web closes its cached service before attempting recovery.

### Coordinator Tests

Pure and injected-runtime tests cover every state and transition, including:

- binding, session, and user-input configuration precedence;
- retained valid session with and without a vault;
- wrong-password and invalid-credential clearing behavior;
- automatic TOTP and MFA challenge response;
- remote identity mismatch with no local mutation;
- cancellation/crash after saved session and resume directly to import;
- import retry versus reconnect-required reauthentication;
- stale and duplicate state-version submissions;
- process-local attempt expiry;
- cross-process provider-connect exclusion;
- counts-only progress and cancellation; and
- runtime installation into an already open profile service.

The Cobra presenter is migrated before either new UI and retains its tested output, prompt, range,
and import behavior through the coordinator.

### Architecture Tests

`internal/provider/architecture_test.go` is extended to enforce:

- `internal/profilecatalog` imports no `internal/provider` package;
- only `internal/onboarding` and command factory wiring import
  `internal/provider/monarch` for this slice;
- `internal/store/**` imports no provider package;
- `internal/provider/**` imports no store package; and
- TUI, API, and web presenters do not import Monarch authentication or credential-persistence
  implementations directly.

### TUI and Parity Tests

Bubble Tea tests cover selector navigation and direct keys, focusable disabled providers, local-only
open, recovery/upgrade guidance, masked inputs, cursor feedback, progress, cancellation, reconnect,
and the transition into and out of the existing finance model.

Python selector, backend-selection, credential-setup, and credential-unlock semantic frames do not
exist yet. The implementation deliberately drives those Python screens and runs
`make parity-update-python`. The resulting artifact diff is reviewed separately before commit.
Python frames constrain semantic content, ordering, labels, and keyboard behavior; reviewed Go
frames remain canonical for resolved terminal styling.

### API and Browser Tests

API and Chromium tests cover:

- selector bootstrap at a non-root base path;
- stable profile-keyed app and API routes;
- two tabs using different profiles concurrently;
- profile-bound mutation tokens and attempt IDs;
- start/submit/cancel mutation security and credential-blind status;
- Huma problems never echo secret request bodies;
- keyboard-only selector, setup, recovery, retry, and reconnect flows;
- direct bookmark reload with analytical query state;
- cached-service eviction before recovery; and
- `--demo` and `--profile` redirects into the one profile-routing model.

### Live Dogfooding

Automated verification is completed and committed before using a real Monarch account. Manual
dogfooding then checks login, TOTP, saved-session restart, progress, import, reconnect, TUI handoff,
web handoff through the configured tailnet/base-path deployment, and preservation of real profile
data during ordinary use. Any correction found by dogfooding is a new tested commit; the verified
automated change is never left dirty while awaiting manual evidence.

## Four Ordered Implementation Plans

This design is implemented through four separate plans. Each plan ends with a green repository and
its own verified commits.

1. **Home locks, profile catalog, and recovery.** Add portable locks, manifests, discovery,
   name/ID resolution, legacy handling, Add cancellation cleanup, and crash-resumable recovery.
2. **Onboarding coordinator and Cobra migration.** Lift provider connection out of Cobra, prove the
   state machine through the existing CLI contract, add profile-aware provider connect/disconnect,
   and keep the command test suite green.
3. **TUI selector and wizard.** Replace the pre-opened-service runner seam with the top-level
   router, add selector/setup/recovery/reconnect screens, capture/review Python semantic artifacts,
   and enter the existing finance model without restart.
4. **Web multi-profile routing and wizard.** Add profile-scoped API/server lifecycle, kit-ui
   selector and credential screens, secure job polling, profile-keyed URLs, and update demo/editing
   browser journeys.

Plan two is the risk-reduction boundary: the renderer-neutral coordinator must reproduce the
working advanced CLI contract before either interactive presenter depends on it.

## Completion Criteria

The slice is complete when:

- a clean install can create, authenticate, import, and enter a Monarch profile entirely inside
  `moneyflow tui`;
- the same complete flow works inside `moneyflow web` at a configured base path;
- keyboard-only use is complete in both interactive selectors and wizards;
- compatible existing root-level profiles remain usable without relocation;
- older/corrupt preview profiles can be explicitly recreated with an intact owner-private backup;
- newer profiles and unknown manifests are never offered destructive recovery;
- local-only profiles remain browseable and are never silently overwritten by provider binding;
- a finance view can reconnect an expired provider session without returning to the selector;
- `provider connect monarch` and `provider disconnect monarch` target the same named catalog;
- profile names and secrets are absent from logs, status payloads, problems, fixtures, and parity
  artifacts;
- the diff contains no database migrations, provider write-back, YNAB/SimpleFIN adapters, or
  general profile management; and
- the complete Go, web, security, parity, race, and repository quality gates pass.
