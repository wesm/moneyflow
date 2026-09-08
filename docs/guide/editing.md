# Editing, review and recovery

Edits are staged in the profile's durable journal. The TUI, browser and MCP see the same pending
work. Closing a process does not discard it. Staging changes the effective analytical view;
committing makes those changes part of committed state or starts provider write-back.

## Stage a change

| Key | Action |
| --- | --- |
| `m` | Rename a merchant or reassign selected transactions |
| `c` | Change category |
| `C` / `G` | Manage categories / category groups where supported |
| `h` | Toggle hidden state where supported |
| `x` | Confirm and stage transaction deletion |
| `u` / `U` | Undo / redo pending work |
| `w`, then Enter | Review and begin committing the reviewed revision |

Selection wins over focus. A bulk action is one undo unit. A new edit after undo discards the redo
tail. Repeating a hide toggle cancels pending toggles when every target has one; cancellation
preserves the remaining batch grouping.

Local and Amazon profiles support local taxonomy management. Monarch and YNAB impose provider
restrictions; a disabled action explains why. Do not assume a selector can create a remote category.

## Review, then commit

Press `w` to inspect pending changes and Enter to apply that reviewed revision. Canceling review
does not discard edits. If another process changes the profile, review again; stale confirmation
does not automatically replay your action.

Local/Amazon commit folds active operations atomically and clears history, including the inactive
redo tail. Monarch/YNAB commit prepares a durable batch. Watch its progress: the command returning
or the dialog changing does not prove every remote item was applied.

## A batch needs attention

Writing may pause for reconnect, rate limiting or a failed item. The UI and MCP status explain
which actions are available. Pause is durable; restart does not silently undo it. Resume is offered
for retryable cases. Stop and reconcile reads remote truth and abandons failed/unsent intent from
the frozen prefix; already-applied remote changes remain applied.

Ordinary edits, undo and refresh are blocked while an unfinished write batch owns the pending
prefix. Browse and committed export remain available. Do not reset the database to dismiss a
batch: use its recovery actions and preserve backups. See [provider behavior](../architecture/providers.md).

## Duplicate review

Press `D` to inspect duplicate candidates in the filtered result. Candidates match on exact date,
amount, account and Unicode-lowercased un-suffixed merchant label. They are suggestions, not proof
that an expense is invalid. In the overlay use Space to select, `i` for details, `h` where allowed,
`x` to stage deletion, and Esc to return. Review and commit deletions through the ordinary flow.
