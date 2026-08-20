---
name: orchestrate-infera-tasks
description: Use when the user provides a sizable multi-service requirement for the infera project (phrasings like "帮我实现/拆分/分解...需求", "按服务拆一下", "拆成任务并去执行", "orchestrate", "端到端做一下这个 feature") and expects both task creation in CoolVibe and automated follow-through execution.
---

# Orchestrate Infera Tasks

Turn one big requirement into: ordered tasks in CoolVibe → layered parallel execution → merged feature branch → verified working tree.

## Constants
- CoolVibe project: `infera` (`project_id = c22f7d20-cd67-41e6-bfa2-909e8f789439`)
- Executor: `CLAUDE_CODE`
- All CoolVibe operations use the `coolvibe_*` MCP tools that are already available.

If either the project name or executor no longer matches the user's setup, confirm with the user before continuing.

## Workflow

### 1. Understand the requirement
- Read the user's full requirement message and any referenced docs/files.
- Identify concrete service / module boundaries (frontend app, backend service, worker, shared lib, infra, docs) from the directory structure — top level plus one level into obvious monorepo containers (`apps/`, `services/`, `packages/`), directory names only — and AGENTS.md / CLAUDE.md. Do not read source files at this stage. Prefer boundaries that already exist in the code over invented ones.
- If the requirement is ambiguous in a way that changes the task split (e.g. two mutually exclusive product directions), ask **one** focused clarifying question. Otherwise proceed.

### 2. Split into ordered tasks
Produce a task plan that respects service and responsibility boundaries, with an explicit dependency order:

- Each task must be **independently mergeable** — a coherent slice a single agent can finish (schema change, one service API, one UI screen, docs update, etc.).
- Group tasks into **layers**. Tasks in the same layer have no dependency on each other and can run in parallel. Each subsequent layer depends only on prior layers finishing.
- Typical layer shape (adapt to the actual request):
  1. Contracts / shared types / DB schema / config.
  2. Backend services / APIs / workers that consume layer 1.
  3. Frontend / clients / SDKs that consume layer 2.
  4. Integration glue, docs, telemetry, cleanup.
- **Contracts materialize in the lowest layer**: cross-task shared items (DB schema, public interface / API signatures, stage enums, event shapes) are defined once and committed by a layer-1 task. Later layers **reference** the committed code instead of re-defining it. At plan time you can only fix the *semantic* contract (what is shared, its boundary, which task materializes it) — never the exact code signature, because the code does not exist yet.
- Every task title should read as an actionable deliverable (e.g. `后端: 新增 /skills 列表接口`), not a vague phase (e.g. `后端改造`).
- Prefix every task title with a unique, monotonically-increasing layer key so titles never collide across requirements. Generate one batch timestamp `B` = `yyyyMMddHHmm` (local time at orchestration start) for the whole requirement, then name each task `L{B}-{layer}-T{nn}` — `{layer}` is the 1-based layer number and `{nn}` is the 2-digit task sequence. Example: `L202608182139-1-T01 后端: 新增 /skills 列表接口`. Do not restart numbering at `L1`; the timestamp keeps each requirement's titles unique.
- Every task description must include:
  - **Scope**: what files/services are in-scope, and a hard **Out of scope** list of what the agent must not touch.
  - **Depends on**: task titles (or "none") — used for scheduling.
  - **Contract**: the semantic contract this task depends on or exposes, written as a pointer + freeze rule, not a pre-invented signature. Example: "approval entry converges to a single method with complexity + split switches, materialized by T01; read the committed signature in `server/internal/api/router.go` and match it — do not add a parallel entry point." Never write an exact signature that has not been implemented yet.
  - **Acceptance**: bullet list of concrete acceptance criteria.
  - **Feature branch**: `feature/<slug>` — the shared integration branch for this requirement. The task's CoolVibe session bases its work on it, and the attempt is merged into it by the orchestrator (step 6). Do **not** tell the task agent to push directly to `feature/<slug>` — same-layer tasks pushing to the same branch would clobber each other.
- Before presenting the plan (step 3), run a **change-surface overlap check** — three tiers, escalate only when needed:
  1. **Path-level, zero code reading (default)**: compare the in-scope directories/files of same-layer tasks using the plan text plus the directory layout from step 1. Same-layer tasks with disjoint directories → pass. No code inspection at all.
  2. **Overlap found → serialize**: when two same-layer tasks share a directory or file, make one depend on the other and push it to a later layer. Do not read code to "check whether it is really a conflict" — accept the latency cost.
  3. **Symbol-level inspection only on explicit user request**: only when the user insists two overlapping tasks must stay parallel, dispatch one read-only Explore subagent (Agent tool, `subagent_type: Explore`) for just that pair; it returns the files/signatures each task would modify and whether they actually collide. Never read the source yourself.
  - Residual conflicts that slip through are handled at merge time (step 6): any non-trivial conflict stops and asks the user.

### 3. Human review gate (MANDATORY — hard stop)
Before writing anything to CoolVibe, present the full plan to the user for review and **wait for explicit approval**:

- Render it as a numbered list grouped by layer, one line per task showing: `title` — one-sentence scope — depends on.
- Attach the shared `feature/<slug>` branch name.
- Explicitly ask the user to review, and list the acceptable responses:
  - `ok` / `go` / `继续` / `同意` / `批准` — approved as-is, proceed to step 4.
  - Free-form feedback — treat as revision requests: adjust titles, scope, ordering, layering, or drop/merge tasks, then re-present the updated plan and ask again.
- **HARD STOP**: Do NOT call `coolvibe_create_task`, `coolvibe_start_workspace_session`, or any other mutating tool until the user has replied with an explicit approval keyword. Silence, "thanks", "看起来不错", or any non-approval message is NOT approval — ask again.
- Every time the plan is revised, treat the previous approval as void and re-request approval on the new version.
- If the user pushes back on the split repeatedly, ask a targeted clarifying question about the disputed boundary rather than guessing.

### 4. Create the tasks in CoolVibe
Only enter this step after the explicit approval from step 3.

For each planned task, call `coolvibe_create_task` with:
- `project_id`: the infera constant above.
- `title`: the actionable deliverable.
- `description`: the structured description from step 2, including the shared `feature/<slug>` branch name.

Collect the returned `task_id` values keyed by title so later steps can reference them.

### 5. Schedule execution (layered parallel)
Process one layer at a time; within a layer, launch all tasks in parallel.

Before launching the first layer, create the shared branch yourself — never rely on CoolVibe to create it: from an up-to-date default branch, `git checkout -b feature/<slug>`, then `git push -u origin feature/<slug>`. Every workspace session then bases on the existing branch.

For each task in the current layer:
1. Call `coolvibe_start_workspace_session` with:
   - `task_id`: from step 4.
   - `executor`: `CLAUDE_CODE`.
   - `repos`: one entry per repo in the project. Get them via `coolvibe_list_repos` once at the start; use the shared `feature/<slug>` branch as `base_branch` for every task so all attempts land on the same integration branch.
2. Record the returned session identifiers.

After launching the layer:
- Poll task status with `coolvibe_get_task` for the current layer's tasks only — do not re-poll `coolvibe_list_tasks`, which returns every task in the project with its full step-2 description re-injected into context on every cycle. Continue until every task in the layer has finished its attempt (`has_in_progress_attempt` false and `status` not in `todo` / `inprogress`).
- CoolVibe does **not** move a task to `done` when its agent finishes — a finished attempt leaves the task in `inreview` / `pendingmerge` ("code produced, awaiting merge"). Reaching `done` is your job (step 6): merge the attempt onto `feature/<slug>`, verify, then set `done` via `coolvibe_update_task`. The status is never updated automatically by the executor.
- If a task ends with `last_attempt_failed=true` or is still `todo`/`inprogress` after a reasonable wait, surface the failure to the user with the task title + id and ask how to proceed (retry, skip, or abort) before starting the next layer.

Only advance to the next layer once the current one is fully **green** — every task in the layer has a finished (non-failed) attempt, is merged onto `feature/<slug>`, passes verification, and is set to `done`.

### 6. Consolidate on the feature branch and reconcile status
Consolidate incrementally: merge each task's attempt onto `feature/<slug>` as soon as that task's session finishes — do not batch all merges to the very end. This does not launch the next layer early; layer gating (step 5) still applies:

1. Ensure the local working copy is on `feature/<slug>` and up to date (`git fetch`, `git checkout feature/<slug>`, `git pull`).
2. Merge each task's attempt branch into `feature/<slug>` in dependency order, resolving conflicts and stopping to ask the user if a conflict is non-trivial.
3. Verify the merged result before marking anything done: run lint/typecheck + unit tests (the first two items of step 7) on the merged branch. The full sequence including the diff review runs once at the end (step 7).
4. For each task whose work is now merged and verified, explicitly set its status to `done` via `coolvibe_update_task`.

Reminder from step 5: `done` is never set automatically — call `coolvibe_update_task` for each task only after its merge is verified.

### 7. Verify
On the consolidated `feature/<slug>` branch, run — in this order, stopping on the first failure:
1. Lint / typecheck (use the project's configured scripts; check `package.json`, `pyproject.toml`, `Makefile`, etc.).
2. Unit tests (project's configured test command).
3. Local code review: invoke the `code-review` skill on the diff `feature/<slug>` vs the base branch. Commit any leftover uncommitted changes first so the reviewed diff is complete.

Report each command and its result. If a step fails, do not silently retry — summarize the failure and the offending task(s), and ask the user whether to fix in-place or reopen a task.

### 8. Wrap up
Deliver a final summary with:
- Feature branch name.
- Table of tasks with `id`, title, final status, and the layer they belonged to.
- Verification results (lint, tests, review).
- Any follow-ups the user should decide on (merge to main, open PR, etc.). Do **not** push to main or open a PR unless the user explicitly asks.

## Guardrails
- The orchestrator session never reads repository source files inline. All code inspection (repo reconnaissance, overlap matrix, anything needing file contents) goes through read-only subagents that return conclusions, not file dumps. This session holds orchestration state only.
- The step 3 review gate is non-negotiable: no CoolVibe writes and no workspace sessions before explicit user approval of the plan.
- Never invent a `project_id`; always use the infera constant or re-verify via `coolvibe_list_projects`.
- Never start workspace sessions in parallel across layers — layer boundaries exist to avoid conflicting edits on the shared branch.
- Within a single layer, tasks must touch disjoint files/symbols. Two tasks that both change the same public method signature or its tests belong in different layers with an explicit dependency, not side by side.
- Never mark a task `done` while an attempt is still in progress, or before its work is merged onto `feature/<slug>` and verified.
- Contracts freeze in the lowest layer: once a layer-1 task commits a shared signature or schema, later tasks must read and match it, not redefine it. If a later task genuinely needs to change a contract, it stops and reports the change as a serial contract-revision task instead of silently editing it in parallel.
- Never force-push, never push to `main`/`master`, never skip pre-commit hooks.
- Task titles must carry the unique timestamp layer key `L{B}-{layer}-T{nn}` (see step 2). Never fall back to a bare `L1`/`L2`… numbering, which would collide with titles from a different requirement.
- If the user's requirement is small enough for a single task, say so and stop — this skill is for multi-service work, not trivial edits.
