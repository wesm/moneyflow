# Go Port YNAB Read, Import, and Refresh Design

**Date:** 2026-08-30

**Status:** Approved

**Branch:** `go-port`

## Purpose

Moneyflow is becoming a full Go replacement for the Python package. The Go application already
has a shared application core, durable SQLite profiles, TUI and web renderers, provider-neutral
refresh orchestration, profile onboarding, Monarch read/write support, Amazon import, export, and
MCP access.

This design covers the next provider slice: connect one Moneyflow profile to one YNAB budget,
import a complete budget snapshot, browse and edit it offline, and reconcile later refreshes
against durable pending edits. The TUI, web interface, CLI, and MCP server consume the same
profile and application-service behavior.

This is intentionally the read/connect/import/refresh half of YNAB support. YNAB write-back is a
separate immediate follow-on slice. Both slices are completed before starting another provider.
Until write-back lands, staged operations are the durable representation of user intent and
cannot be committed on a YNAB-backed profile.

## Relationship to Existing Contracts

This slice builds on the approved designs:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`
- `2026-08-14-go-port-sqlite-editing-design.md`
- `2026-08-15-go-port-monarch-read-refresh-design.md`
- `2026-08-17-go-port-profile-catalog-onboarding-design.md`
- `2026-08-18-go-port-monarch-write-back-design.md`
- `2026-08-21-go-port-mcp-design.md`

Those documents remain authoritative for exact money, stable local identity, label collision
handling, provider refresh leases and generation checks, deletion confirmation, journal rebase,
profile selection, renderer security, privacy, and install-only schemas. This document states
only YNAB-specific behavior and the small shared-onboarding changes required by a second
interactive provider.

The Python YNAB implementation remains the behavioral oracle where its behavior is sound. Named
differences below are deliberate. This document does not authorize removing Python, beginning
YNAB write-back, pushing the branch, or adding another provider.

## Goals

- Connect a pristine Moneyflow profile to one selected YNAB budget using a personal access token.
- Store the token in a profile-scoped, password-encrypted vault outside SQLite.
- Import accounts, payees, category groups, categories, transactions, and split details from one
  coherent full-budget response.
- Preserve YNAB milliunit amounts exactly as integer Moneyflow minor units without floating-point
  arithmetic or silent rounding.
- Import uncleared transactions and expose their provider-pending state.
- Preserve parent-level split behavior for parity while retaining complete split detail for a
  later split-aware accounting and editing slice.
- Reconcile complete YNAB snapshots through the existing provider refresh, deletion-confirmation,
  stable-identity, journal-rebase, and revision contracts.
- Support provider setup, unlock, offline open, refresh, and reconnect through both TUI and web.
- Provide equivalent explicit CLI connect and disconnect commands.
- Keep the MCP server offline-capable and renderer-neutral without teaching it credential entry.
- Demonstrate full reconciliation with 100,000 transactions within the established performance
  gates.
- Preserve the no-CGO Linux, macOS, and Windows portability contract.

## Non-Goals

- Creating, updating, or deleting data through the YNAB API.
- Committing staged changes on a YNAB profile before the write-back slice.
- YNAB delta synchronization through `server_knowledge`.
- Importing scheduled transactions as committed transactions.
- Projecting split lines as independent accounting transactions.
- Editing split lines, split-aware analytics, or split-aware write-back.
- A generated OpenAPI client or a third-party YNAB Go SDK.
- A general-purpose provider plugin system.
- Sharing one YNAB token vault among profiles or budgets.
- Automatically selecting YNAB's `last-used` budget for a new profile.
- Automatically unlocking credentials on process restart.
- Persisting a plaintext token, account password, or decrypted credential material.
- Importing Python YNAB caches or credentials.
- Schema or credential-vault migrations while Go v2 remains install-only.
- A compatibility read path for schema version 10 profiles.
- Provider network work while a profile is opened explicitly offline.

## Core Decisions

1. One Moneyflow profile binds to exactly one YNAB budget. The selected YNAB plan ID is the
   immutable remote profile identity.
2. Refresh uses one complete budget export. Delta synchronization is deferred until measurements
   show that full reconciliation is no longer acceptable.
3. The adapter is a small direct `net/http` client inside `internal/provider/ynab`. Huma continues
   to define Moneyflow's inbound web API; it is not used as an outbound YNAB client.
4. Each profile owns its own password-encrypted YNAB token vault. The token is decrypted only into
   process memory after an interactive unlock.
5. YNAB milliunits are converted exactly to the selected budget's ISO currency and decimal scale.
   Nonrepresentable or overflowing values reject the entire candidate.
6. All ordinary transactions returned by the full budget endpoint are imported, including
   `uncleared` transactions. Scheduled transactions are not imported.
7. A split parent remains one Moneyflow transaction at the parent amount. Every returned split is
   also retained losslessly in a provider-owned detail table.
8. Full reconciliation reuses the existing atomic provider-refresh fold. The network response is
   normalized outside SQLite; identity allocation, authoritative rebase, validation, and fold
   happen through the existing closed transactional callback.
9. Provider write-back remains unavailable in this slice. Pending edits persist across restarts
   and refreshes until the write-back slice is installed.
10. The shared onboarding coordinator becomes provider-neutral, while Monarch and YNAB retain
    provider-specific state machines and secret payloads.
11. Provider session sources are capability-sized. The current combined `provider.Source` is split
    into read and write source interfaces; Monarch implements both and YNAB implements only the
    read source in this slice.

## Named Python Parity Decisions and Divergences

The consolidated parity decisions in earlier designs remain in force. This slice adds:

1. **Budget selection is explicit.** Python selects the first returned budget unless a budget ID
   is supplied. Go automatically selects only when exactly one budget is available; otherwise the
   user chooses. List ordering never changes the binding.
2. **Each budget gets its own profile and vault.** Python uses one configured backend instance.
   Go stores one immutable budget binding per profile, allowing multiple budgets to coexist without
   shared mutable credential state.
3. **The token is password-encrypted.** Python places the personal access token in its generic
   password field. Go stores it in a versioned Argon2id/AES-256-GCM vault protected by the
   Moneyflow account password.
4. **Full budget export replaces many SDK calls.** Python fetches accounts, payees, categories,
   and transactions through separate SDK endpoints and caches them in process. Go uses the YNAB
   full-budget response as one coherent refresh candidate and persists the result in SQLite.
5. **Uncleared transactions remain visible and are explicitly pending.** This matches Python's
   mapping of `cleared == uncleared` to pending state and differs from the Monarch policy that
   omits unposted rows.
6. **Transfers and tracking-account transactions remain hidden by default.** This matches the
   Python transformation. The provider facts are still stored and can be inspected.
7. **Split parents remain one visible transaction.** This matches Python's parent-level accounting.
   Go additionally retains complete split payloads so the later split-aware slice does not require
   a destructive schema rethink.
8. **Colliding external labels remain distinct.** Python's string-keyed payee model can conflate
   different YNAB payee IDs with identical labels. Go preserves each identity and uses the shared
   sticky display-suffix policy.
9. **No automatic first-budget fallback survives reconnect.** A missing bound budget is an identity
   mismatch, never permission to select another budget.
10. **Provider-backed commit is deferred.** Python can update and delete YNAB data. This slice
    stages and reviews edits but disables commit until the YNAB write-back contract is approved.
11. **No generated SDK is shipped.** Python depends on the generated `ynab-python` package. Go
    ports only the two read endpoints used by this slice, reducing binary and API surface.
12. **Deleted transactions are absent, not hidden rows.** Python converts a returned deleted
    transaction into a hidden transaction. YNAB documents deleted entities as delta-only, and Go
    uses complete non-delta responses, so a deleted transaction in a full response is invalid and
    an absent transaction is reconciled as a deletion.

## Architecture and Dependency Direction

```text
CLI connect ────────────────────────────────────────┐
                                                   │
Bubble Tea TUI ────────────────────────────────────┤ provider-neutral onboarding/actions
                                                   v
Svelte web ── Huma inbound API ───────────> internal/onboarding + internal/app
                                                    /                 \
                                      provider contracts             store
                                               │                       │
                                       YNAB REST adapter          SQLite profile
                                       encrypted vault            + journal
```

`internal/provider` continues to define neutral provider identities, snapshots, progress, sources,
and error codes. It imports only `internal/domain`. Its source boundary becomes:

```go
type SourceState interface {
    Changed(SessionFingerprint) (bool, error)
}

type ReaderSource interface {
    SourceState
    Reader(context.Context, bool) (Reader, SessionFingerprint, error)
}

type WriterSource interface {
    SourceState
    Writer(context.Context, bool) (Writer, SessionFingerprint, error)
}
```

The application refresh runtime accepts `ReaderSource`; the provider-write runtime separately
accepts `WriterSource`. Monarch's source satisfies both. YNAB satisfies only `ReaderSource` until
the write-back slice. No optional method, nil writer, capability type assertion, or unsupported
writer stub is introduced.

`internal/provider/ynab` owns YNAB HTTP requests, bearer authentication, bounded response decoding,
wire types, exact milliunit conversion, snapshot normalization, token-vault payloads, and provider
error translation. It imports `internal/provider`, `internal/domain`, and shared hardened-file or
sealed-vault mechanics. It never imports `internal/store`, `internal/api`, or renderer packages.

`internal/onboarding` owns the provider-neutral attempt lifecycle: profile-scoped connection lock,
attempt ID, state version, cancellation, progress, rollback, and credential-blind snapshots. Small
provider-specific drivers own legal transitions and secret inputs. Only onboarding and command
factory wiring import concrete provider packages.

`internal/app` orchestrates provider refresh and store operations. It does not inspect YNAB JSON or
decrypted token payloads.

`internal/store` imports no provider implementation. SQL rows and transaction handles never escape
the store. The store owns external-identity mappings, split-detail persistence, bindings, leases,
refresh generations, journal rebase, and the atomic fold.

`internal/api` remains Huma-based. It exposes presenter-neutral onboarding and refresh operations;
it does not become a YNAB proxy.

The existing architecture tests are extended to enforce these directions. In particular,
`internal/provider/ynab` cannot import store or API packages, and TUI/web presenters cannot import
the YNAB adapter.

## Package and Source Layout

The tree lists slice-relevant additions and changed seams, not packages to remove:

```text
cmd/moneyflow/                  ynab connect/disconnect and runtime factories
internal/credentialvault/       shared sealed-file envelope mechanics
internal/provider/              neutral read contracts and stable errors
internal/provider/ynab/         REST transport, wire mapping, vault, and source
internal/onboarding/            neutral coordinator and provider-specific drivers
internal/domain/                import snapshot and split-detail values
internal/app/                   refresh, capabilities, and runtime wiring
internal/store/                 split-detail and atomic refresh contracts
internal/store/sqlite/          schema v11 and fold implementation
internal/profilecatalog/        local-only YNAB status derivation
internal/api/                   Huma onboarding/refresh schemas
internal/tui/                   YNAB selector, wizard, unlock, and progress
web/                            YNAB selector, wizard, unlock, and progress
```

The shared credential package is justified now because Monarch and YNAB need the same hardened
Argon2id/AES-GCM envelope mechanics. It does not own provider payloads, paths, user-facing errors,
or credential policy. The Monarch payload and additional authenticated data remain unchanged; the
refactor must read existing Monarch vaults byte-for-byte without migration or fallback formats.

## Minimal YNAB REST Surface

The read slice implements only:

- `GET /plans` to validate the token and list available budgets.
- `GET /plans/{plan_id}` to fetch one complete budget export.

The base URL defaults to `https://api.ynab.com/v1`. Tests inject an `httptest.Server`, clock,
random source, and transport. The production base URL is not a user-facing flag.

Every request:

- uses `Authorization: Bearer <token>`;
- uses context cancellation and a five-minute total request deadline;
- permits ordinary JSON content types only;
- reads through a 512 MiB hard response limit;
- rejects a nonempty trailing JSON value;
- tolerates unknown JSON object fields for forward-compatible additive API changes; and
- validates every field Moneyflow consumes before returning a neutral snapshot.

The adapter makes one HTTP attempt. Retry policy belongs to the application scheduler, except that
one bounded `Retry-After` value is carried through `provider_rate_limited` using the existing
provider contract. No generic retry wrapper is introduced.

The full-budget endpoint deliberately replaces Python's separate list calls. It returns plan
metadata, accounts, payees, category groups and categories, transactions, subtransactions, and one
`server_knowledge` value in a single document. The value must be a nonnegative integer and is
discarded after validation in this slice. It is never persisted or sent as a delta cursor.

## Provider Error Translation

Raw response bodies, request URLs containing identifiers, tokens, and YNAB error details never
leave `internal/provider/ynab`.

The adapter maps:

- HTTP 401 and 403 to `provider_reconnect_required`;
- HTTP 429 to `provider_rate_limited`, preserving only a valid bounded `Retry-After`;
- network failures and HTTP 5xx to `provider_unavailable`;
- a missing bound budget or returned plan-ID mismatch to `provider_identity_mismatch`;
- malformed JSON, missing required fields, broken references, impossible split totals, and exact
  money conversion failures to `provider_data_invalid`; and
- context cancellation to the caller's cancellation result without wrapping provider payloads.

This slice adds `provider_money_mismatch` for a valid remote budget whose ISO currency or decimal
scale differs from the immutable local binding. It is manual-action-required and never retries
automatically. The user-facing response explains that the budget needs a new profile with the
correct money interpretation; it contains currency codes and scales, never amounts or labels.

Every provider code belongs to exactly one scheduler class. The existing table-driven exhaustivity
test gains the new code.

## Budget Identity and Profile Binding

Moneyflow calls the user-facing object a **YNAB budget**. The current API names it a plan and uses a
`plan_id`; that opaque ID is the binding's `remote_profile_id`.

A profile can bind only when it satisfies the existing pristine predicate:

- current schema;
- no committed account, transaction, taxonomy, or merchant rows;
- no journal operations or redo tail;
- revision zero; and
- no existing provider binding.

Creating or selecting a budget never replaces local state. An unbound non-pristine profile remains
Local only and the wizard offers Open Offline or Cancel.

When exactly one budget is returned, onboarding selects it automatically. When more than one is
returned, the user must choose. The response order has no semantic meaning. Choices are sorted by
Unicode-lowercased display name, then opaque remote ID bytewise for deterministic presentation.

Presenter snapshots do not expose the remote ID. Each choice carries an attempt-scoped opaque
`choice_id`, display name, and last-modified date when supplied. Plan summaries may include a
nullable currency format, but Moneyflow deliberately derives the binding from the selected full
plan so identity, currency, data, and server knowledge come from one coherent document. The
submitted choice ID is valid only for the same attempt, profile, state version, and server process.

Binding and the first snapshot fold occur in the same authoritative store transaction. A failure
leaves the profile unbound and without imported rows. Once bound, every refresh first verifies that
the exact plan ID remains available to the token and that the full response has that same ID.
Moneyflow never substitutes `last-used`, the first returned budget, or another budget after binding.

## Exact Money and Currency Binding

YNAB transaction and split amounts are signed 64-bit integer milliunits: one thousandth of a major
currency unit. Moneyflow stores signed integer minor units at the budget's declared
`currency_format.decimal_digits` scale.

At initial binding:

- `currency_format.iso_code` must be exactly three uppercase ASCII letters;
- `decimal_digits` must be between 0 and 9;
- the selected currency and scale are shown for confirmation before import; and
- the confirmed values become immutable profile binding fields.

Conversion never uses `float32` or `float64`:

- scale 3 stores the milliunit value unchanged;
- scale below 3 requires exact divisibility by `10^(3-scale)` and divides using integers;
- scale above 3 multiplies by `10^(scale-3)` with checked overflow; and
- a non-divisible or overflowing amount rejects the complete candidate.

The exact rule applies independently to parent transactions and every split line. The adapter also
formats the canonical exact decimal amount string through the shared domain money formatter; no
JSON or SQLite money column is REAL.

Every refresh compares the current remote ISO code and scale with the binding before planning.
A mismatch returns `provider_money_mismatch` and changes no profile, journal, revision, generation,
or operational success timestamp.

## Credential Vault

Each YNAB profile uses:

```text
<profile-root>/providers/ynab/credentials.enc
```

The file is a provider-specific versioned payload sealed by the shared credential-vault envelope.
The payload contains only:

- payload version;
- personal access token;
- selected budget ID after selection;
- selected budget currency and scale after confirmation.

The payload never contains the Moneyflow account password. The envelope uses the established
Argon2id parameters, a random salt and nonce, AES-256-GCM, provider-specific authenticated data,
owner-only files, hardened path traversal, bounded reads, and atomic replacement. Wrong-password
and tamper failures remain deliberately indistinguishable.

The SQLite provider binding is authoritative after the first import transaction commits. The
budget, currency, and scale copied into the vault are consulted only while the profile is unbound,
so an initial import failure can resume without repeating selection. For a bound profile, a vault
whose budget ID differs from SQLite produces `provider_identity_mismatch`; differing currency or
scale produces `provider_money_mismatch`. Moneyflow never rewrites the binding from vault contents.

The token is held only in process memory after unlock. Secret byte buffers are cleared after
handoff where Go permits. The account password is never cached. A process restart therefore makes
the profile locally **Locked** for network work while leaving all committed data available offline.

The vault fingerprint is process-local runtime state. If a CLI command atomically replaces or
deletes the vault, a TUI or web process notices on its bounded status tick. It clears its old YNAB
runtime and prompts for unlock or reconnect; it never blindly retries authentication with stale
material. A replaced encrypted vault cannot heal another process until that process receives the
account password interactively.

`provider disconnect ynab` deletes only `credentials.enc`. It preserves the profile binding,
committed data, journal, mappings, and split details. A long-lived process drops its in-memory token
when it observes the deletion. Offline use remains available; network actions show reconnect
guidance.

## Provider-Neutral Onboarding Coordinator

The current coordinator's attempt lifecycle is retained:

- one OS advisory `provider-connect.lock` per profile;
- attempt IDs bound to process instance and profile ID;
- state-version compare-and-swap on every submission;
- credential-blind polling snapshots;
- cancellation and crash-safe restart behavior;
- counts-only progress; and
- rollback of an abandoned newly created profile when no durable provider state remains.

Protocol version increments from 1 to 2. Version 2 adds:

- state `remote_profile_required`;
- action `select_remote_profile`;
- bounded `remote_profiles` choices in snapshots;
- provider-specific secret input envelopes selected by `provider_kind`; and
- local status `locked` for a bound profile whose encrypted vault exists but is not unlocked in
  the current process.

The coordinator is not converted into a generic workflow language. It delegates legal transitions
to one provider driver. Monarch preserves its existing sequence and wire behavior. YNAB supplies a
small driver over the same lifecycle. Presenter code switches on stable states and field schemas,
not concrete adapter types.

All secret-bearing submit endpoints remain Huma mutation operations protected by the current
mutation token, exact-origin, and Fetch-Metadata checks. Status responses never echo the personal
access token, account password, budget remote ID, raw provider response, or request body. Huma
problem responses also remain credential-blind.

## YNAB Onboarding State Machine

For a new profile:

```text
inspect
  -> credentials_required
  -> authenticating
  -> remote_profile_required (only when multiple budgets exist)
  -> settings_required
  -> importing
  -> complete
```

For a profile with a retained vault:

```text
inspect
  -> unlock_required
  -> authenticating
  -> remote_profile_required (only if unbound and multiple budgets exist)
  -> settings_required (only if unbound and not already retained)
  -> importing
  -> complete
```

`credentials_required` asks for:

- YNAB personal access token;
- Moneyflow account password; and
- confirmation of the Moneyflow account password.

`unlock_required` asks only for the Moneyflow account password. The provider token is a password
field in all renderers and always displays a clear input indicator without revealing characters.

Authentication first calls `GET /plans`. No vault is saved until the token is valid. If one budget
exists it is selected; otherwise the wizard shows choices. Moneyflow then fetches and holds one
complete selected-plan candidate in process memory. That candidate supplies currency and scale,
which the user must explicitly confirm. The waiting state remains `authenticating` with bounded
counts-only progress until the candidate is ready; no extra protocol state is introduced. There are
no free-form currency or scale fields in the YNAB wizard.

After confirmation, the validated token and selected import configuration are saved atomically.
The full initial import then runs. If import fails, the valid vault is retained and the pristine
profile remains unbound. Retrying onboarding asks only for the Moneyflow account password and goes
straight to validation and another import of the retained budget.

Cancel before the vault is durably saved rolls back a newly added empty profile, matching Python's
Add-account behavior. Cancel or process death after vault save leaves Setup incomplete so the user
does not have to paste the token again. Existing profiles are never deleted by cancellation.

A bound profile whose token can no longer see the bound budget enters Identity mismatch; it never
returns to budget selection. A token rejected by YNAB enters Reconnect required and permits new
credentials or cancellation. An unbound but non-pristine profile enters Local only.

On successful completion, TUI and web install the process-local provider runtime immediately and
open the finance view without restart.

## CLI Contract

The explicit commands are:

```text
moneyflow provider connect ynab [--profile <name-or-id>]
moneyflow provider disconnect ynab [--profile <name-or-id>]
```

Profile resolution follows the catalog contract: exact key or ID first, then unique normalized
display name; ambiguity fails; omission chooses the sole persistent profile only when exactly one
exists.

Connect uses the same coordinator as TUI and web. It prompts for the token and account password,
shows a numbered budget chooser when required, confirms the derived currency and scale, reports
counts-only import progress, and prints a final imported-transaction summary. It does not accept
`--currency`, `--scale`, `--budget`, or token flags. Secrets never enter shell history.

After success, stderr names both next steps using the resolved profile selector:

```text
moneyflow tui --profile <selector>
moneyflow web --profile <selector>
```

Disconnect removes only the encrypted vault and reports that existing data remains available
offline. It is idempotent when the vault is already absent.

## Full Budget Normalization

Normalization produces one complete `domain.ImportSnapshot` plus provider-owned YNAB split details.
All IDs are trimmed, nonempty, valid UTF-8 strings of at most 256 bytes. Account, payee, category,
and group labels are valid UTF-8 strings of at most 1,024 bytes. Transaction and split memos are
valid UTF-8 strings of at most 500 Unicode code points. Duplicate external identities reject the
candidate.

Because absence drives deletion, the full response must explicitly contain the accounts, payees,
category groups, categories, transactions, and subtransactions arrays. A missing required array is
not treated as empty; it rejects the candidate as incomplete provider data. The ignored scheduled
arrays may be absent.

### Accounts

Every nondeleted account in the full response receives a stable local account ID. Closed accounts
are retained. Budget and tracking accounts are both imported because historical transactions must
remain complete.

Account type, `on_budget`, `closed`, and `deleted` are validated and used during normalization.
The stable account identity, display label, and transaction-level hidden projection are retained;
this slice does not add a second account-metadata table.

### Payees and Merchants

Every nondeleted payee receives a stable local merchant ID. The raw YNAB payee label is the
provider label used by sticky collision allocation. Separate payee IDs with the same label remain
separate local entities.

A transaction with no payee uses one protected provider-owned `YNAB: Unknown Payee` merchant.
A transaction with a nonempty payee ID that cannot be resolved rejects the candidate.

Transfer payees remain ordinary stable provider identities. A transfer transaction is hidden by
the visibility rule below; its identity is not collapsed into the destination account.

### Category Groups and Categories

Every nondeleted category group and category is imported with stable local IDs, including hidden or
internal groups needed by historical transactions. A category's group ID must resolve.

A nonsplit transaction with no category references the installed protected system category
`category_system_uncategorized`, matching Monarch and local profiles. A split parent references one
new protected system category, `category_system_split`, under the installed
`group_system_uncategorized` group. Split is provider-neutral because later split-aware accounting
can use it for any provider. Neither protected category participates in provider-label collision
allocation or external-ID retirement. A nonempty unresolved category ID rejects the candidate.

The protected system identities cannot collide with an ordinary remote ID and cannot be retired
because a provider omits them.

### Transactions

Moneyflow imports every nondeleted transaction in the full budget response. It does not import the
separate scheduled-transaction collection.

Each transaction requires:

- stable external transaction ID;
- resolvable account ID;
- valid ISO date;
- exactly convertible signed milliunit amount;
- a known `cleared` value; and
- consistent payee, category, and split references.

The normalized row uses:

- merchant from the payee mapping or Unknown Payee;
- category from the category mapping, Uncategorized, or Split;
- memo as notes;
- `Pending = true` exactly when `cleared == uncleared`;
- `Hidden = true` for transfer transactions or transactions whose account is off budget;
- `Hidden = false` for other transactions.

`cleared` values `cleared`, `uncleared`, and `reconciled` are accepted. Unknown values reject the
candidate. A provider-pending row is still a committed Moneyflow row and participates in analytics,
filtering, duplicate detection, export, and later write-back according to ordinary rules.

### Split Transactions

A transaction with one or more subtransactions remains one parent Moneyflow transaction at its
full amount. The full-plan document returns transactions and subtransactions as separate arrays;
Moneyflow joins them by each subtransaction's parent transaction ID. The parent's visible merchant
stays the parent payee or Unknown Payee, and its visible category is Split. Split lines are not
separately aggregated, selected, edited, exported, or returned by the bounded transaction API in
this slice.

Every split in a complete non-delta response must have `deleted = false`. A deleted split rejects
the candidate as an endpoint-contract violation. The sum of all split milliunit amounts must equal
the parent milliunit amount exactly. Every split ID must be unique within the provider snapshot,
its parent ID must resolve to exactly one returned transaction, and any nonempty payee, category,
or transfer references must be valid. A violation rejects the full candidate.

The store retains each split's complete used payload:

- stable external split ID;
- parent stable local transaction ID;
- deterministic position within its parent after sorting that parent's split IDs bytewise;
- signed milliunit amount and exact converted minor amount;
- memo;
- payee ID and label when present;
- category ID and label when present;
- transfer account and transfer transaction IDs when present.

The retained payload is provider detail, not a second accounting source. The parent transaction
remains authoritative until the split-aware slice explicitly changes that model.

## Stable Identities and Label Collisions

External IDs map one-to-one to opaque stable local IDs through the existing identity tables. A
refresh never derives identity from labels, dates, amounts, or array position.

The shared provider-label allocator applies to YNAB accounts, payees, categories, and groups:

- the first observed collision owner keeps the unsuffixed display label;
- later colliders receive a deterministic suffix;
- assignments persist and never reshuffle when another collider appears;
- user-renamed entities are never relabeled by import;
- pending local label operations win in effective state; and
- import never silently merges identities.

The raw provider label remains stored separately from the local display allocation. The later
write-back slice must use provider identity and provider labels, never a collision suffix, when
addressing YNAB.

## Complete Reconciliation

Every refresh fetches `GET /plans/{bound-id}` without a date, account, category, or payee filter and
without a delta cursor. The response is one candidate snapshot. Absence from this exhaustive
response therefore implies remote deletion, subject to the existing plausibility guard.

The refresh sequence is:

1. acquire the provider refresh lease without changing profile revision;
2. unlock or reuse the current process's token runtime;
3. fetch the exact bound plan ID and normalize one full-budget response outside SQLite;
4. treat HTTP 404 as identity mismatch and verify the returned plan ID against the binding;
5. compute the deletion-plausibility decision against the current committed base;
6. enter the existing immediate store transaction;
7. verify no provider write batch exists, the refresh generation is unchanged, and the binding
   still matches;
8. allocate local identities and sticky labels, rebase the current journal, replay effective state,
   and validate references through the closed pure callback;
9. replace provider-owned committed rows and split details atomically;
10. increment profile revision and refresh generation exactly once; and
11. record counts-only success state outside semantic revision accounting.

No SQLite transaction is held during network I/O. The lease coordinates work but is never a
correctness mechanism. The generation compare-and-swap remains authoritative.

An unchanged normalized candidate, unchanged mappings, unchanged split payload, and unchanged
journal produce no semantic revision increment. Operational last-attempt and last-success fields
may change without changing the revision.

## Deletion Plausibility and Confirmation

The existing four-arm deletion guard applies to posted and uncleared YNAB transactions together:

- nonempty to empty always requires confirmation;
- at least 25 removals and at least 10 percent requires confirmation;
- at least 1,000 removals requires confirmation; or
- at least 5 removals and at least 50 percent requires confirmation.

The candidate is bound to the current refresh generation and current process. Confirmation reuses
the exact normalized candidate, enters a fresh write transaction, and reruns authoritative journal
rebase against current state. It never bypasses identity, money, reference, or response-integrity
validation.

Entering confirmation-required releases the lease. Another process may fetch its own candidate.
The existing expiry, wrong-process, and generation-invalidated token behavior remains unchanged.

The full-budget response is not offset-paginated, so Monarch's two-partition count probes and
pagination-stability retries do not apply. A truncated, oversized, malformed, or incomplete JSON
document is a correctness failure and can never be confirmed.

## Journal Rebase

Refresh reuses the exact operation-order and rewrite rules from the Monarch read slice:

- target IDs resolved at staging time remain concrete;
- transaction operations lose targets that no longer exist remotely;
- an operation with no targets is removed;
- partial batch shrink preserves operation identity and ordering;
- the cursor is the count of active operations and adjusts past removed active operations;
- inactive redo operations are rebased and may be removed;
- refresh permanently discards an invalidated redo tail as already specified;
- entity existence is evaluated sequentially, including entities created by retained earlier
  operations; and
- structural operations sweep current membership at replay time.

Uncleared-to-cleared changes do not change local transaction identity because the YNAB transaction
ID remains the identity. If YNAB replaces an ID, reconciliation sees delete plus add; any pending
operation targeting the old ID is removed and announced by counts only.

Refresh remains available at the journal ceiling because rebase never appends a user operation.
Selection revalidation is all-or-nothing: if any selected identity vanished, the complete selection
is cleared and announced.

## Commit and Capability Behavior

YNAB profiles expose ordinary browsing, analytics, search, selection, editing, undo, redo, review,
duplicates, export, transaction information, and MCP read tools.

In this slice:

- `provider.refresh` is available only while a YNAB runtime is unlocked;
- `w` and `commit_changes` report that YNAB write-back is not installed;
- pending operations remain durable across restarts and refreshes;
- review says that changes are staged locally until YNAB write-back is available;
- no local-only commit is offered because a later refresh would overwrite provider-owned values;
  and
- no YNAB `provider.Writer` is declared or implemented.

The action registry keeps static action identities. Availability and help text are capability-driven,
so the write-back slice can enable commit without changing renderer bindings.

## Refresh Scheduling and Process Lifecycle

Once unlocked, a long-lived TUI or web process participates in the existing six-hour full-refresh
cadence. Manual `r` refreshes immediately, subject to the provider lease and existing failures.

Locked, offline, reconnect-required, identity-mismatch, money-mismatch, and deletion-confirmation
states never retry network work automatically. The scheduler records only allowlisted codes,
counts, revision numbers, timings, and correlation IDs.

The process keeps the decrypted token only until profile close, explicit disconnect observation,
runtime replacement, or process exit. An idle-closed web profile service loses its unlocked
runtime and prompts again when network work is next requested.

The MCP process never runs the background scheduler and never prompts for credentials. Against a
locked YNAB profile it may read committed data offline. `refresh_data` returns capability guidance
to unlock the profile in TUI or web; MCP does not accept the vault password or token.

## Renderer Experience

### Profile Selector

The provider selector enables YNAB in TUI and web. Local-only status derivation uses disk and
binding state, never a network call per row:

- bound plus vault present: Ready in the local-only catalog; opening offers Unlock or Open Offline;
- bound plus vault absent: Reconnect;
- unbound pristine plus vault present: Setup incomplete;
- unbound pristine without vault: Setup incomplete;
- unbound non-pristine: Local only;
- older or corrupt schema: Needs recovery; and
- newer schema: Requires newer Moneyflow.

Opening a Locked profile offers Unlock and Open Offline. Opening Reconnect offers Enter token and
Open Offline. A failed refresh from the finance view exposes the same Unlock/Reconnect action
without returning to the selector.

### TUI Wizard

The YNAB wizard uses the existing selector-first shell and keyboard conventions. It provides:

- masked token and account-password inputs with visible entry indicators;
- an accessible numbered budget list with stable focus;
- explicit currency/scale confirmation;
- counts-only `Authenticating with YNAB...` and import progress;
- Cancel at every user-input step; and
- Open Offline for an existing bound profile.

No secret appears in the frame capture or semantic parity artifacts. The wizard is a new Go flow;
Python frames guide structure and wording but secret screens use synthetic placeholders only.

### Web Wizard

The web wizard uses the same Huma onboarding start/submit/cancel/status endpoints and provider-
specific version-2 fields. It uses kit-ui controls, accessible labels, focus restoration, keyboard
submission, and the current no-store/no-CORS/mutation-token/origin protections.

The browser never persists the token or account password in URL state, local storage, session
storage, cookies, service-worker caches, history, analytics, or logs. A reload during import resumes
credential-blind status polling; a reload during an input step requires the secret to be entered
again.

Successful completion redirects to the stable `/p/<profile-id>/` finance route. The running server
installs the unlocked runtime for that profile without restart.

## Huma API Changes

The existing onboarding schemas gain version-2 union fields rather than YNAB-specific endpoints:

- `remote_profiles` on status snapshots;
- `select_remote_profile` action input;
- `ynab_credentials` containing token, account password, and confirmation; and
- provider-specific field requirements selected by `provider_kind`.

Huma validates structural bounds. The coordinator validates state, attempt ownership, expected
state version, and provider-specific semantics. Unknown or cross-provider secret fields are rejected.

Read-only finance endpoints remain unchanged. Refresh, confirmation, and onboarding submissions
keep their current mutation security. No endpoint returns YNAB remote IDs, tokens, raw error bodies,
or full provider payloads.

## SQLite Schema Version 11

The installed schema increments from version 10 to version 11. There is no migration. Existing
preview profiles are refused with ordinary recreate guidance.

Existing generic tables store:

- provider binding with kind `ynab`, remote plan ID, currency, and scale;
- external identities for accounts, merchants/payees, groups, categories, and transactions;
- sticky provider-label allocations;
- refresh lease, generation, status, and confirmation bookkeeping; and
- the ordinary committed base and journal.

Version 11 adds the protected `category_system_split` row and a STRICT provider-owned split table
with the logical shape:

```text
ynab_transaction_splits(
  parent_transaction_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  external_id TEXT NOT NULL,
  amount_milliunits INTEGER NOT NULL,
  amount_minor INTEGER NOT NULL,
  memo TEXT,
  payee_external_id TEXT,
  payee_label TEXT,
  category_external_id TEXT,
  category_label TEXT,
  transfer_account_external_id TEXT,
  transfer_transaction_external_id TEXT,
  PRIMARY KEY(parent_transaction_id, position),
  UNIQUE(external_id),
  FOREIGN KEY(parent_transaction_id) REFERENCES transactions(id) ON DELETE CASCADE
)
```

The full endpoint documents deleted subtransactions as delta-only. A returned deleted split is
therefore rejected before planning and needs no speculative storage branch.

Indexes support parent lookup and external-ID uniqueness. Schema inspection tests assert STRICT
mode, foreign keys, integer money columns, absence of REAL money columns, and version 11.

The store's refresh plan gains complete replacement split rows. Reference and optimized fold paths
must produce the same canonical logical encoding, including split ordering and money values.

## Privacy and Logging

Persisted logs, status rows, import history, progress events, and error envelopes may contain only:

- provider kind;
- stable allowlisted error or reason code;
- entity and transaction counts;
- revision and refresh-generation numbers;
- attempt, pass, and timing values; and
- random correlation IDs.

They never contain token values, budget IDs or names, account names, payee labels, category labels,
memos, transaction dates, amounts, search text, split descriptions, response bodies, or vault paths.

The interactive budget chooser and profile selector may display budget/profile names because they
are intentional user-facing financial UI. Those values never cross into logs or persisted status.
Tests and documentation use synthetic names only.

HTTP request/response dumps are forbidden. Adapter errors are translated at the YNAB package
boundary before they reach the shared logger.

## Performance Budgets

At 100,000 transactions with representative accounts, payees, categories, and split density:

- full JSON decode and normalization: 1 second reference, 4 seconds CI ceiling;
- in-memory plan construction and validation: 500 milliseconds reference, 2 seconds CI ceiling;
- authoritative rebase and SQLite fold: 1 second reference, 4 seconds CI ceiling;
- cold reopen and effective-snapshot build: existing 250 millisecond reference and 1 second CI
  ceiling; and
- TUI and web reprojection after fold: existing bounded-view contracts.

Network duration is reported separately and is not part of CPU/storage gates. The 512 MiB body cap
and five-minute deadline are correctness bounds, not expected performance.

Performance tests run through SQLite and the real JSON normalizer using generated synthetic data.
They never contact YNAB or use the user's default profile.

## Verification Obligations

### REST Adapter

- `GET /plans` and `GET /plans/{id}` use the correct bearer header, paths, and one-attempt behavior.
- 401/403, 429 with bounded `Retry-After`, 5xx, network errors, cancellation, bad content type,
  oversized response, malformed JSON, and trailing JSON map to the specified neutral outcomes.
- Unknown additive JSON fields are tolerated.
- Raw response bodies, token values, remote IDs, and labels do not escape translated errors or
  captured logs.
- The adapter has no dependency on Huma, SQLite, onboarding, or renderers.

### Exact Money

- Milliunit conversion covers scales 0 through 9, positive and negative amounts, zero, exact
  divisibility, nondivisible rejection, and overflow.
- Parent and split values use the same conversion.
- Currency and scale mismatch rejects refresh with no state change.
- Schema inspection finds no REAL money column.
- JSON and SQLite round trips preserve exact minor units and exact decimal strings.

### Normalization

- Closed, on-budget, off-budget, transfer, cleared, uncleared, reconciled, uncategorized, and split
  transactions normalize as specified.
- Uncleared rows are imported with `Pending = true`; cleared and reconciled rows are false.
- Transfer and off-budget rows are hidden; ordinary on-budget rows are visible.
- Scheduled transactions are ignored even when the response contains them.
- Separate same-label payees/categories/accounts remain separate stable entities with sticky suffixes.
- Missing nonempty references, duplicate identities, unknown cleared values, invalid dates, invalid
  UTF-8, or oversized fields reject the complete candidate.
- Protected Unknown Payee and system Uncategorized/Split entities cannot collide with provider IDs.

### Split Retention

- A split parent projects as one transaction at the parent amount and Split category.
- Every split field is retained with deterministic position and exact money.
- Split sums must equal the parent exactly; mismatch rejects the candidate.
- A deleted split in a complete response rejects the candidate and changes no state.
- Duplicate split IDs, wrong parent IDs, and unresolved split references reject the candidate.
- Refresh replacement removes obsolete split rows atomically with the parent base.
- Split rows never appear as independent analytics or export transactions in this slice.
- Restart preserves the complete split payload.

### Binding and Vault

- Zero, one, and multiple-budget onboarding flows follow the stated selection rules.
- Attempt-scoped choice IDs cannot be reused across attempts, profiles, state versions, or processes.
- A bound budget missing from a later list produces identity mismatch and no fallback.
- Binding accepts only the pristine predicate and is atomic with first import.
- Wrong password, tamper, truncation, oversize, symlink redirection, and permissive ACL failures are
  handled through existing hardened-file behavior on Linux, macOS, and Windows.
- Two profiles may hold different encrypted tokens/budgets without shared state.
- The Monarch vault still reads existing version-1 fixtures after shared-envelope extraction.
- Vault replacement/deletion invalidates a process runtime without exposing secrets.

### Onboarding and Renderers

- Protocol version 2 preserves every Monarch version-1 flow while adding YNAB states.
- CLI, TUI, and web all drive the same coordinator and produce the same state transitions.
- TUI and web token/password fields are masked but visibly accept input.
- Status polling and Huma problems are credential-blind.
- Cancel before vault save removes a newly created empty profile; cancel after vault save retains
  Setup incomplete and permits unlock-only retry.
- Successful import installs the process runtime without restart.
- Bound profiles can open offline without a token or unlock.
- YNAB appears as available in provider selectors and help.
- Web tests cover keyboard submission, focus restoration, reload during import, and base-path routes.

### Refresh, Rebase, and Concurrency

- Full refresh uses one complete budget response and no delta cursor.
- The deletion threshold boundary matrix and confirmation-token invalidation matrix remain green.
- Refresh fold refuses while a provider write batch exists.
- The refresh-generation compare-and-swap rejects a stale candidate even if lease discipline fails.
- Exactly one of two concurrent folds succeeds.
- Reference and optimized refresh plans have the same canonical persisted logical state.
- Randomized journal sequences prove full replay equals incremental application before and after
  refresh rebase.
- Missing targets shrink operations and adjust the cursor; structural operations sweep current
  membership.
- Selection clears all-or-nothing when any selected transaction vanishes.
- No-op refresh changes only operational timestamps, not committed tables or semantic revision.
- Refresh remains available at the journal ceiling.

### Scheduler and Capabilities

- The provider error classification table assigns every code to exactly one retry class.
- `provider_money_mismatch`, reconnect, identity mismatch, data invalid, and deletion confirmation
  require manual action.
- Rate limits honor only bounded retry metadata.
- TUI and web run the six-hour cadence only while the YNAB runtime is unlocked.
- Offline, locked, and MCP processes do not start background provider work.
- Commit is unavailable with truthful staged-intent guidance.

### Live Characterization

An explicitly enabled live test uses a user-supplied token from the environment and a disposable
temporary Moneyflow home. It never writes YNAB data and never records provider payloads. It verifies:

- a selected plan ID remains stable across repeated list and full-plan requests;
- the full-plan response covers historical transactions from closed and tracking accounts;
- the currency ISO code and decimal digits match the selected budget display;
- transaction IDs remain stable across two reads;
- uncleared transactions, when the chosen budget has any, retain their IDs and cleared state;
- split IDs, parent links, and sums are internally consistent; and
- one full response is sufficient to join every retained account, payee, category, and group
  reference.

The test reports only counts and pass/fail conditions. It is never part of ordinary CI and never
creates a committed fixture. Automated synthetic verification is committed before live dogfooding;
anything found live lands in a later verified commit.

## Completion Criteria

This slice is complete when:

- a new profile can connect to a selected YNAB budget through CLI, TUI, and web;
- the token is stored only in a profile-scoped password-encrypted vault;
- a complete snapshot imports accounts, payees, taxonomy, uncleared/cleared transactions, and split
  details into schema version 11;
- the profile browses, searches, edits, reviews, exports, and serves MCP reads offline;
- manual and six-hour refreshes reconcile remote truth through generation CAS, deletion
  confirmation, and deterministic journal rebase;
- TUI and web show the same provider state, progress, failures, and recovery actions;
- all architecture, unit, store, race, parity, web, and 100,000-row gates pass;
- live read-only dogfooding confirms the named API assumptions;
- no personal data or generated live fixture is committed;
- the diff contains no YNAB mutation endpoint, `provider.Writer`, delta-sync path, plaintext token,
  generated SDK, CGO dependency, or schema migration; and
- the verified implementation is committed before manual confirmation.

## Immediate Write-Back Follow-On

After this slice is approved and implemented, the next YNAB design adds provider write-back. It
must resolve at least:

- absolute transaction update and delete items in the durable write-batch model;
- YNAB payee rename, including its all-transactions cascade;
- rename-to-existing payee behavior and duplicate-label ambiguity;
- category assignment and supported taxonomy mutations;
- delete semantics and not-found characterization;
- provider rules overriding requested fields;
- pending/uncleared transaction write policy;
- split-parent and split-line edit restrictions;
- YNAB's lack of a direct hide-from-reports transaction field;
- exact response reconciliation, crash-uncertain outcomes, and idempotency; and
- any write-specific rate-limit behavior.

The read slice deliberately avoids declaring a writer interface or schema shape before those
semantics are reviewed.

## Implementation Decomposition

Implementation is planned as independently green checkpoints:

1. **Transport and exact normalization:** direct REST client, typed wire values, error mapping,
   budget listing, full response decoding, milliunit conversion, and synthetic fixtures.
2. **Vault and onboarding generalization:** shared sealed-envelope mechanics, YNAB vault, protocol
   version 2, provider-specific drivers, and CLI migration onto the shared coordinator.
3. **Schema and atomic fold:** version 11 split table, store plan values, identity allocation,
   full reconciliation, no-op behavior, and restart tests.
4. **Application runtime:** YNAB source, capabilities, refresh scheduling, deletion confirmation,
   journal rebase, and performance gates.
5. **TUI experience:** provider selection, token/unlock/budget/settings screens, progress, offline
   open, reconnect, and semantic frames.
6. **Web experience:** Huma version-2 schemas, kit-ui wizard, locked/offline flow, progress,
   keyboard behavior, base-path routing, and browser tests.
7. **Integration and dogfood:** architecture/race/privacy/parity suites, full verification, then the
   explicitly enabled read-only live characterization with a user-provided token.

Each checkpoint uses test-first implementation and ends in a verified commit. Schema version 11
and every query that depends on it land in the same commit. No checkpoint leaves agent-authored
changes uncommitted at handoff.
