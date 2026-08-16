# Monarch Scoped Import and Label Normalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore Python-compatible `--mtd` Monarch loading and accept provider display labels
that Python already imports safely.

**Architecture:** Keep date scope in the Monarch adapter options and GraphQL page request; do not
persist it in session credentials or the provider-neutral interface. Allow a scoped snapshot to
seed only a pristine profile, while ordinary refresh remains exhaustive. Canonicalize provider
labels at the adapter boundary before the provider-neutral snapshot validator runs.

**Tech Stack:** Go, Cobra, Monarch GraphQL, SQLite, `testing`, Testify.

## Global Constraints

- Stay on `go-port`; do not create or switch branches.
- Use integer minor units and never round provider money.
- Do not log or persist provider labels, transaction data, or other financial details.
- Write failing tests before production changes.
- Commit and rebuild `bin/moneyflow` before handoff.

---

### Task 1: Canonicalize Provider Labels

**Files:**

- Modify: `internal/provider/monarch/snapshot.go`
- Test: `internal/provider/monarch/snapshot_test.go`

**Interfaces:**

- Consumes: Monarch account, merchant, category, and category-group display labels.
- Produces: `normalizeProviderLabel(string) (string, error)` and domain-valid import entities.

- [x] Add a snapshot test whose provider label contains internal control characters and assert
      that the normalized snapshot contains a single ordinary space without leaking the raw value.
- [x] Run the focused test and verify it fails in `ImportSnapshot.Validate`.
- [x] Implement provider-boundary normalization: replace Unicode control characters with spaces,
      trim Unicode edge whitespace, and preserve ordinary display characters.
- [x] Run all Monarch provider tests.

### Task 2: Add Python-Compatible Month-to-Date Loading

**Files:**

- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `internal/provider/monarch/client.go`
- Modify: `internal/provider/monarch/client_test.go`
- Modify: `internal/provider/monarch/pagination.go`
- Modify: `internal/provider/monarch/snapshot_test.go`
- Modify: `internal/provider/monarch/session_file.go`

**Interfaces:**

- Consumes: Cobra `--mtd`, the command clock, and `monarch.Options.TransactionStartDate` /
  `TransactionEndDate`.
- Produces: GraphQL `filters.startDate` and `filters.endDate` on both visible and hidden page reads.

- [x] Add failing Cobra tests proving `--mtd` calculates the first day of the current local month,
      reaches the Monarch factory, imports a pristine profile, and refuses a populated profile.
- [x] Add failing client tests proving both date bounds accompany `hideFromReports` in every page
      request and invalid half-ranges are rejected.
- [x] Thread a non-persisted snapshot range through the command factory, source options, and page
      request without changing saved credential/session formats.
- [x] Print a scoped import summary while retaining the existing progress format.
- [x] Run focused command and Monarch tests.

### Task 3: Verify and Commit

**Files:**

- Modify: `docs/superpowers/specs/2026-08-15-go-port-monarch-read-refresh-design.md`
- Modify: `docs/superpowers/plans/2026-08-15-go-port-monarch-read-refresh.md`

**Interfaces:**

- Consumes: the completed behavior and test evidence.
- Produces: documentation that records scoped pristine seeding and provider-label canonicalization.

- [x] Update the existing Monarch design and implementation plan with the Python-parity behavior.
- [x] Run focused tests, `make verify-go`, and documentation checks.
- [x] Review the diff and scan it for private data.
- [x] Commit with a conventional human-readable message.
- [x] Run `make build` and verify the working tree is clean.
