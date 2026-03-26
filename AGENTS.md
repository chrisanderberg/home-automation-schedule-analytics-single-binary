# AGENTS.md

## Purpose
- This file defines how agents should operate in this worktree.
- Read `PROJECT.md` for project context and goals.
- Read `REQUIREMENTS.md` for the current implementation contract.

## Source of truth
- `PROJECT.md` and `REQUIREMENTS.md` together are the effective project spec.
- If `PROJECT.md` and `REQUIREMENTS.md` conflict, `REQUIREMENTS.md` wins for implementation details and constraints.
- `REQUIREMENTS.md` contains both the active requirements and the rationale-bearing design guidance that used to live in separate decision records.

## Requirement handling
- Hard requirements are binding. Do not violate them unless the human explicitly changes or waives them.
- Soft requirements are preferred guidance. Follow them by default, but you may deviate when there is a clear task-specific reason.
- Agents may add or refine soft requirements when they discover reusable guidance during implementation.
- Humans may add or revise hard and soft requirements.
- Agents must not silently create, remove, or weaken hard requirements.
- If a soft requirement appears important enough to become mandatory, add it to the candidate promotion section in `REQUIREMENTS.md` instead of promoting it directly.
- Durable rationale should be recorded directly with the relevant soft requirement in `REQUIREMENTS.md`, not in a separate decision log.
- Do not use `REQUIREMENTS.md` as a scratchpad or status log. Add only reusable guidance or binding constraints.
- When requirements emerge from prototypes or examples, capture them in `REQUIREMENTS.md` in a reusable form.
- Keep `REQUIREMENTS.md` concise and reviewable. Do not add task-specific notes that will not matter to future work.

## Execution rules
- If something is not specified, do not guess silently. Surface the gap or make the narrowest safe assumption, and record that assumption in `REQUIREMENTS.md` under `Open questions` or an `Assumptions` section if one is needed.
- Prefer parameterization over hardcoding when requirements are still evolving.
- Keep code structure reviewable. Split large functions into focused helpers.
- Add brief comments only where intent or invariants are not obvious from the code.

## Milestone workflow
1. Scaffold plus tests.
2. Implementation.
3. Code Review Loop.

For each milestone:
- Create file structure and tests that define expected behavior.
- Ensure `go test ./...` runs during scaffolding, even if some tests fail because implementation is pending.
- Complete implementation until tests pass.
- Run code review tools, fix issues, and rerun until code review comes back clean.

## Definition of done
- `go test ./...` passes.
- No placeholder TODO sentinels remain in production code unless explicitly approved.
- Non-obvious logic has comments explaining intent or invariants.
- Any new reusable implementation guidance discovered during the task is added to the soft requirements in `REQUIREMENTS.md`.

## Commands

```bash
go test ./...
make build
make test
make run
```
