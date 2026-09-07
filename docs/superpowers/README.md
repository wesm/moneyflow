# Design and implementation history

The maintained Go architecture lives in [docs/architecture](../architecture/index.md).
Start there for current ownership, invariants, provider behavior, interface contracts, and tests.

Completed documents here preserve how the port was designed and implemented. Their non-goals,
schema versions, command names, review checkpoints, and verification instructions describe their
slice at the time; later implementation often superseded them. In particular, full Python/Go
frame-parity artifacts and their regeneration targets have been retired.

- [Current architecture decisions and provenance](../architecture/decisions.md) maps the lasting
  choices to their original specs.
- `plans/` is implementation history, not a queue of work to rerun.
- `benchmarks/` contains historical measurement evidence, not claims about every current machine.
- [YNAB write-back](specs/2026-09-07-go-port-ynab-write-back-design.md) is an active draft under
  review. It has not been implemented or converted into current architecture.

Keep a proposal separate until its implementation lands. Update the maintained architecture with
the resulting behavior, then mark the completed proposal historical. Do not let a new dated spec
become the only place an application's durable contract is documented.
