# AGENTS.md

## Authority and scope
- This file is the single source of truth for how to work in this repo.
- Domain spec is canonical in `planning/single-bin/spec.md`.
- Plan is canonical in `planning/single-bin/plan.md`.
- Assumptions are canonical in `planning/single-bin/assumptions.md`.
- Do not introduce new semantics that contradict the domain spec.

## Milestone execution rule (two-step)
Each milestone must be implemented in two steps:

1. **Scaffold + tests**
   - Create file structure and stubs.
   - Add tests that define correct behavior.
   - Ensure `go test ./...` runs (tests may fail if stubs are TODO).

2. **Implementation**
   - Fill in TODOs until tests pass.
   - Ensure `go test ./...` passes before completing the milestone.

## Milestone Definition of Done
Before marking a milestone complete:
- `go test ./...` passes.
- No placeholder TODO sentinels remain in production code unless explicitly
  approved.
- Non-obvious logic has comments explaining intent/invariants.
- Code structure is reviewable (large functions split into focused helpers).

## Assumptions and TBD handling
- If something is not specified, do not guess silently.
- Prefer parameterization when possible.
- Record any necessary assumptions in `DECISIONS.md`.

## Commands

```bash
go test ./...
make build
make test
make run
```

## Canonical documents
- Domain spec: `planning/single-bin/spec.md`
- Plan: `planning/single-bin/plan.md`
- Assumptions: `planning/single-bin/assumptions.md`
- Decisions: `DECISIONS.md`
