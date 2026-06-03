# Reactor — Operating Scenarios & Pluggable Persistence

> Reactor design doc: the four operating scenarios, the pluggable persistence split
> (`ItemStore` / `ConfigStore` / `LedgerStore` + the flow execution layer on top), and the
> tracker → Reactor transition. All open questions are settled — see **Decisions locked** at the
> end; milestones **M0–M7** track the build.

## Context

Reactor is the OSS successor to the private `tracker` project — `github.com/promise-language/reactor`, dual Apache/MIT, sibling to `flow` and `forge`. Empty repo today (licenses only). **All of promise / flow / forge / reactor go public together** in quick succession; until then, cross-repo deps use git submodules + `replace` (no piecemeal opening).

It **retains** tracker's web UI + runner/governor, **runs flows unchanged**, **adopts the forge blueprint** (`./make` builds every dev tool into `bin/`; `bin/verify` commit gate; ratcheted `.baselines.json`; guard hook), and — the major change — **replaces tracker's single repo-backed `Store` with a persistence layer split by concern.**

A guiding principle: **push domain logic to the project, keep Reactor thin.**
- **Flows are normal forge build tools** living in the project: `tools/flow` → `bin/flow`. A flow is self-describing (declares its own item types/eligibility); it carries **no Reactor configuration**.
- **Gates likewise move to the project** — the project exposes its gate registry via a discovery command (`bin/gate list --json`); see [Gate discovery](#gate-discovery--project-defines-reactor-schedules) below. Reactor keeps gate *scheduling/execution/history* (plus deployment-side overrides like arena assignment), not gate *definitions* or *metric semantics*.
- Reactor provides **SDKs** (the flow SDK; a gate SDK) for core functionality and stays out of domain specifics.

## The four scenarios

### 1 — Admin production line  *(must-support; ≈ tracker today)*
A maintainer drives a large backlog across a **farm of hosts/arenas** (permanent + ephemeral cloud arenas) — a production line resolving items in waves with continuous quality via periodic gates. **The Reactor server runs in the cloud, not locally** (running it locally is a recipe for trouble); it *manages* local + ephemeral cloud arenas but the server itself is cloud-hosted, with **admin accounts / access control**. Few admins (1 now, maybe a couple soon).

### 2 — Manual contributor  *(donate AI via CLI; no server)*
A contributor clones the **promise project**, runs `./make` (builds `bin/flow` — flows are project tools, **no external deps**), then runs `bin/flow resolve`: claim one eligible issue, resolve it, open a **PR** for an admin to review/merge. **No Reactor server, no concurrency orchestration.** **All validation gates run locally in the contributor's worktree.** May run a couple of flows concurrently by hand.

### 3 — Power contributor  *(auto loop)*
Same as #2 but unattended: `bin/flow auto` runs `resolve` in a **loop until a stop condition** (quota reached, cost cap, no eligible items). **Serverless and self-capped** by the contributor's own quota/limits, on their own machine/worktree — between #1 and #2. *(A serverless CLI loop, not a local Reactor server — Reactor is cloud-only.)*

### 4 — Admin PR intake  *(process contributor PRs)*
**Same cloud Reactor and same arena farm as #1** (no separate server per sub-role). Admins run security/completeness review flows + **cross-platform gates** (e.g. 3 OSes contributors can't run — ephemeral cloud arenas are likely the practical way to run these). Outcomes branch: **merge**, or **return-to-sender** for more work.

## Cross-cutting axes

| Axis | #1 Admin line | #2 Manual CLI | #3 Auto loop | #4 PR intake |
|---|---|---|---|---|
| Server | **cloud** (admin) | none | none | **same cloud server as #1** |
| Orchestration | heavy (waves+gates) | `bin/flow resolve` | `bin/flow auto` loop | review flows + gates |
| Item source of truth | **GitHub issues** | GitHub issues | GitHub issues | GitHub issues + PRs |
| Arenas | farm; perm+ephemeral | local worktree | local worktree | farm; perm+ephemeral |
| Gates | scheduled (cloud) | **local, in worktree** | local, in worktree | cross-platform (cloud) |
| Config/limits | deployment-owner | n/a | contributor-local | deployment-owner |
| Claim coordination | GH assignee/labels + ledger | GH assignee + `.flow/active.json` | GH assignee | GH assignee/labels + ledger |

## Architecture mapping

### ItemStore — composite identity (GitHub) + private overlay

GitHub is the single **identity** authority; a private overlay store is keyed by that identity — *not* two competing populations to sync (which would inevitably leak/mix). One item, loaded by merging layers:

- **Identity + public state = GitHub issues/PRs** (the SoT; visible to everyone). Issues = work definitions; **PRs are first-class items with their own identity** (their github PR number), because multiple contributors may produce different PRs for one issue and a security review of PR-A must **not** apply to PR-B. Public fields ↔ labels/assignee/state/body; small public artifacts ↔ flow's orphan `flow/artifacts/issue-N/…` branch.
- **Private overlay = `RepoItemStore` (or KV), keyed by the GitHub id.** Holds admin-only / large artifacts that shouldn't or can't live on the public issue (the project can't host large capacity for everyone, and GitHub issues can't be deleted). Lives with the admin cloud Reactor.

So `ItemStore` is a small CRUD interface with these implementations:
- `GitHubItemStore` — go-github issues+PRs (the identity layer).
- `RepoItemStore` — tracker-compatible `.tracker/{id}.json`; used **as the overlay keyed by GitHub id** on a GitHub deployment, **and** as the standalone identity store for GitHub-free (private/offline) deployments.
- `CompositeItemStore` — merges GitHub identity + overlay on load; what the admin Reactor uses.

**One identity authority per deployment — GitHub *or* repo, never both for the same items** — so nothing leaks/mixes across worlds. The repo overlay (keyed by GitHub id) only ever *adds* admin/private artifacts to a GitHub-identity deployment; it is never a second source of item identity.

### The other two stores (kept thin)

All three ride one **minimal record core** (`Get/Put/Delete/List(ns)` + `Filter(ns,pred)` + `Search(ns,q)`) so backends stay swappable.

- **`ConfigStore`** — deliberately minimal residual: things the project *can't* own because they're the **deployment owner's** choice — quota/cost limits, Claude tokens/auth, arena allocation + provider creds, admin access control. (Flows and gates are **not** here — they live in the project.)
- **`LedgerStore`** — per-server **active** records: lease ledger, gate run history/baselines, orchestration/scheduler run state, turn registry, quota snapshot, notifications, the GitHub read-index cache. CRUD-shaped, hot. Impls: repo (`_*.json`) + KV example.

### flow.Backend — execution layer on top of ItemStore

flow's own interface (claim/release · load-state · resolve-artifact · worktree — *not* CRUD). Two impls:
- **`github` — flow's `pkg/backend/github`, reused verbatim** (scenarios #2/#3, and the contributor-facing parts; lease in `.flow/active.json` / GH assignee).
- **`reactor` adapter** — for the admin server: composite-aware, writes large/private artifacts to the overlay (keyed by GitHub id) and public state to GitHub; implements flow's optional `RefResolver`/`Finalizer`/`StateInspector`. (= `pkg/backend/tracker` from tracker's `oss-flow-migration.md`.)

### Single-issue work, first-class PRs

```
issue (work def, flow:resolve)
   ├─ contributor A: resolve ─▶ PR #a  (first-class item; own review artifacts)
   └─ contributor B: resolve ─▶ PR #b  (separate identity; separate review)
each PR ──admin review/security (flow:review)──▶ cross-platform gates ──▶
     merge  OR  return-to-sender (PR/issue back to open + notes)
```
Admin review = **more flows on the PR item**; artifacts attach to the **PR's identity** (on GitHub if public, else the private overlay). Eligibility routing via `flow:<binary>` labels + assignee: contributors run `flow:resolve`; admins run `flow:review`/`flow:gate`.

### Gate discovery — project defines, Reactor schedules

Tracker required each gate to be entered by hand into `ConfigStore` (name, command, schedule, host filter, metric directions, ratchet caps…). That doesn't scale to a multi-project Reactor and forces a maintainer to mirror project knowledge into the server. Reactor flips the relationship: **the project declares its gates; Reactor discovers them.**

**The contract.** A project exposes a single command — convention: `bin/gate list --json` — that emits a manifest describing every gate it offers and a global `preflight` command. The manifest is the source of truth for gate *identity*, *runtime*, *eligibility*, and *metric semantics*. Reactor periodically re-runs the command (on adoption, on PRs that touch the manifest, and as a refresh tick) and reconciles.

**Manifest shape** (v1, JSON):

```json
{
  "schema_version": 1,
  "project": "promise",
  "preflight": {
    "default": "./make",
    "windows": ".\\make.cmd"
  },
  "gates": [
    {
      "name":            "promise-tests",
      "command": {
        "default": "bin/gate test",
        "windows": "bin\\gate.exe test"
      },
      "host_os":         ["linux", "darwin", "windows"],
      "host_arch":       ["amd64", "arm64"],
      "timeout":         "30m",
      "schedule":        "every 4h",
      "allow_dirty_tree": false,
      "tags":            ["tests", "host"],
      "metrics": [
        { "name": "test_count",    "type": "int", "direction": "up",   "mode": "enforced",      "cap": 10000 },
        { "name": "test_failures", "type": "int", "direction": "down", "mode": "enforced",      "cap": 0     },
        { "name": "leak_count",    "type": "int", "direction": "down", "mode": "enforced",      "cap": 0     },
        { "name": "excluded_count","type": "int", "direction": "down", "mode": "informational"               }
      ]
    }
  ]
}
```

Field semantics:

| Field | Meaning |
|---|---|
| `schema_version` | Major version; Reactor refuses unknown majors. |
| `preflight` | Optional global setup command Reactor runs after a fresh checkout, before any gate (e.g. `./make` to build `bin/gate` itself, sync submodules, sanity-check the tree). OS-dispatched (see note). |
| `gates[].name` | Stable id; keys metric history and baselines. **Must be unique within the manifest.** |
| `gates[].command` | Exec line. OS-dispatched (see note). |
| `gates[].host_os` | `linux` / `darwin` / `windows` / `any`. Eligibility filter. |
| `gates[].host_arch` | Optional `amd64` / `arm64` filter (lets a project target "linux arm64" separately from "linux amd64" without a target-triple grammar). Omitted ≡ any. |
| `gates[].timeout` | Go duration (`30m`, `2h`). |
| `gates[].schedule` | Vocabulary tracker already uses: `every <dur>`, `daily`, `weekly`, `after-every-commit`, `manual`. |
| `gates[].allow_dirty_tree` | Skip the post-run clean-tree check. |
| `gates[].tags` | Free-form; attached to auto-filed bugs. |
| `gates[].metrics[]` | One spec per metric the gate emits — see below. |

**OS-dispatched commands.** `preflight` and `gates[].command` each accept either a **string** (used on every OS) or an **object** `{ "default": …, "linux": …, "darwin": …, "windows": … }` — the host-OS key wins, `default` is the fallback. OS keys use the same vocabulary as `host_os`. (A bare string is shorthand for `{ "default": … }`.)

**Metric spec.** `{name, type, direction, mode, cap?}`:

- `type`: `int` / `float` / `bool` (bool persisted as 0/1; direction must be `down` for "has-X" invariants).
- `direction`: `up` (higher is better) or `down` (lower is better).
- `mode`: `enforced` (regression fails the gate), `pending` (recorded, doesn't fail), `informational` (recorded only — no enforcement, never causes regression).
- `cap` (optional): direction-aware ceiling/floor at which baseline auto-ratcheting stops. Prevents a one-time fluke (e.g. coverage spiking on a partial run) from making every future run "regress". `up` → baseline ≤ cap; `down` → baseline ≥ cap.

### Gate output envelope

Every gate writes a `GateOutput` JSON object to stdout (progress to stderr) — the envelope is mandatory (Reactor has no exit-code-only mode; this is a deliberate simplification). Single-target invariant: one gate run = one target. The envelope carries the target, a flat `metrics` map keyed by the names declared in the manifest, optional per-file test groups for granular history, and a `complete` marker. See the project's `docs/gate-output.md` for the authoritative schema; Reactor consumes it without parsing the gate's human-readable output.

### Reactor-side gate config (retained)

The manifest is *project knowledge*. Reactor retains **deployment-side** config keyed by `(project, gate_name)`, layered on top of the manifest:

- **Arena assignment** — which arena (image, provider, persistent vs ephemeral, IAM/service-account) runs the gate. The project says "I need linux/amd64"; Reactor decides *which* linux/amd64 arena.
- **Manual overrides** — disable a gate, narrow `host_match` further (e.g. only on the macbook in the corner), force a different `schedule`, raise/lower a `cap`, switch a metric from `enforced` → `pending` during an incident, grant temporary `GateException`s. Overrides never *add* metrics or change `direction` — those are gate-contract concerns owned by the project.
- **Metric history + baselines** — per-host run history, baselines used for regression detection, ratchet state. Stays in `LedgerStore` exactly as today.

Layering rule: **manifest defines the contract; deployment overrides constrain or annotate it.** Reactor never silently invents fields the project didn't declare.

### Discovery lifecycle

1. **Adopt.** First time a worktree is registered, Reactor runs `preflight` (per host OS), then `bin/gate list --json`, validates the manifest, and creates a `Gate` record per entry (merging any existing deployment-side overrides keyed by name).
2. **Refresh.** On each new commit (or a slow tick — every few hours), Reactor re-runs the manifest command. New gates → adopted with defaults. Removed gates → marked retired (history preserved, scheduling stopped). Changed `direction`/`type` → flagged for admin review (changing metric semantics mid-history would invalidate baselines).
3. **Execute.** Reactor's existing scheduler/runner picks eligible gates per `host_os` × `host_arch` × deployment overrides, runs `preflight` if the worktree is fresh, then the gate command, parses the `GateOutput`, and writes results to `LedgerStore`.

### Per-scenario wiring

| | Server | ItemStore | flow.Backend | Config/Ledger |
|---|---|---|---|---|
| **#1** | cloud admin | Composite (GitHub + overlay) + read-index | reactor adapter | deployment-owner + full ledgers |
| **#2** | none | GitHub only (flow talks straight to GH) | github (verbatim) | n/a |
| **#3** | none | GitHub only | github (verbatim) | contributor-local caps |
| **#4** | same as #1 | Composite (PRs first-class) | reactor adapter | deployment-owner + ledgers |

## Tracker → Reactor transition

Sequenced; each milestone independently buildable; tracker-compatible on disk through M2.

- **M0 — Bootstrap.** `go mod init …/reactor`; fix `LICENSE`; forge tooling → `./make` + `bin/verify` + `.baselines.json` + guard (replaces `make.sh`/`verify.sh`/`deploy.sh`); flow+forge via submodule/`replace` (→ versioned when all go public).
- **M1 — Define the seam.** Record core + `ItemStore` (composite-capable) / `ConfigStore` (thin) / `LedgerStore` interfaces + the two filter/search helpers; compile-time assertions.
- **M2 — Repo-backed parity (the engine).** Lift tracker's `store.go`/`lease_ledger.go`/`_*.json` into the three interfaces, byte-compatible on disk; port UI + runner/governor + orchestrator/scheduler/gate-execution/quota onto them. Cloud-deployable. **Delivers #1 (repo-backed).** Verify parity.
- **M3 — Flow layer + flows as project tools.** `reactor` flow.Backend adapter + flow's `pkg/backend/github` verbatim; relocate flows to `tools/flow`→`bin/flow` (forge tool); binaries select backend by config. **Delivers #2** (`bin/flow resolve`, gates local in worktree).
- **M4 — GitHub ItemStore + composite overlay.** `GitHubItemStore` (issues + first-class PRs) + private overlay keyed by GitHub id + local read-index. Unifies #1 on GitHub; adds **`bin/flow auto`** (#3).
- **M5 — Admin PR-intake lifecycle (#4).** First-class PR items; per-PR review/security flows + cross-platform gates on ephemeral cloud arenas; merge / return-to-sender; PR-open signal/webhook routing.
- **M6 — Gates → project.** Adopt the [gate-discovery](#gate-discovery--project-defines-reactor-schedules) contract: Reactor reads `bin/gate list --json` from each project worktree, runs the declared `preflight` before gate execution, consumes the `GateOutput` envelope, and keeps deployment-side overrides (arena assignment, manual narrows, exceptions) layered on top. Ships the project-side helper as a gate SDK. Existing `gate_set`-style writes migrate to discovery-driven adoption; manual create/delete from the UI is retired (overrides remain).
- **M7 — KV example + conformance + forge resilience + cleanup.** KV Config/Ledger backends + one cross-backend conformance suite; route atomic-write/flock/retry through forge `primitives`; cut `.mcp.json`→example, systemd `deploy.sh`→generic, B-prefix shim (if no legacy data), scrub legacy comments. `go mod tidy`. **Cloud arena stays.**

**Access control:** the cloud server needs real admin auth (per-admin accounts vs tracker's single bearer token) — tracker's `docs/https-oauth-plan.md` is the starting point; scope to the few-admins case.

## Decisions locked

- **GitHub issues = unified source of truth** (no sync world); space can bifurcate later if ever needed.
- **PRs are first-class items** with their own identity; review artifacts are per-PR.
- **ItemStore = one identity authority per deployment** (GitHub *or* repo, never mixed) **+ optional repo overlay** keyed by GitHub id for admin/private/large artifacts.
- **Reactor is cloud-only**; **#1 and #4 share one cloud server** (no per-sub-role servers); needs admin accounts/access control.
- **Scenario 3 = `bin/flow auto`** — a serverless, self-capped CLI loop (no local server).
- **Flows and gates are project-owned forge tools** (`tools/flow`→`bin/flow`; gates declared via `bin/gate list --json`, see [Gate discovery](#gate-discovery--project-defines-reactor-schedules)); Reactor ships SDKs and keeps execution/scheduling/history plus deployment-side overrides (arena assignment, manual narrows, exceptions).
- **Keep cloud arenas** (mostly implemented; likely the practical way to run cross-platform gates).
- **All repos (promise/flow/forge/reactor) go public together**; submodules + `replace` in the interim.
- Build tooling is the **forge blueprint** (`./make`, `bin/verify`, ratcheted baselines, guard) — not `make.sh`.
