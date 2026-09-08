# Amazon order imports

Choose Amazon when adding a profile. Confirm currency and scale, optionally clone committed
taxonomy from another profile, then select Amazon order-history CSV files or a directory.
The initial clone, binding and import complete atomically. Taxonomy cloning is a one-time copy,
not a live link to the source profile.

In an existing Amazon profile, `r` opens import source selection. There is no background Amazon
refresh or reconnect scheduler. The CLI also exposes `provider import amazon`; consult its
`--help` for file/directory and profile options.

## Re-import and corrections

An observed order's non-cancelled item multiset is authoritative. Orders absent from an export
are not deleted. Cancelled rows still mark their order observed and can retire previously imported
items. Exact fingerprints and unambiguous singleton pairing preserve identity; ambiguous
replacements do not move your categories onto an unrelated purchase.

Imports validate currency against the binding. Invalid active rows reject the candidate and give
in-session file/record/column coordinates; persisted history remains counts-only. An unchanged
re-import leaves financial state and revision unchanged.

## Local refinement and matching

Amazon tables label Merchant as Product and Account as Order. Edits and taxonomy management commit
locally; nothing is written back to Amazon. Keep backups of these local changes.

Transaction details can match qualifying Amazon charges against compatible local Amazon profiles.
Exact matches take precedence across all candidate profiles; fuzzy candidates are suggestions.
Matching has a date window, exact-money tolerances and a bounded result list. It never links or
edits a transaction merely because a candidate appears. See [matching rules](../architecture/providers.md).
