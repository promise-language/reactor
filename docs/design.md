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
- **Gates likewise move to the project** (forge `project.toml` gate registry). Reactor keeps gate *scheduling/execution/history*, not gate *definitions*.
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
- **M6 — Gates → project.** Move gate *definitions* to project `project.toml` + a gate SDK; Reactor reads + schedules + records history.
- **M7 — KV example + conformance + forge resilience + cleanup.** KV Config/Ledger backends + one cross-backend conformance suite; route atomic-write/flock/retry through forge `primitives`; cut `.mcp.json`→example, systemd `deploy.sh`→generic, B-prefix shim (if no legacy data), scrub legacy comments. `go mod tidy`. **Cloud arena stays.**

**Access control:** the cloud server needs real admin auth (per-admin accounts vs tracker's single bearer token) — tracker's `docs/https-oauth-plan.md` is the starting point; scope to the few-admins case.

## Decisions locked

- **GitHub issues = unified source of truth** (no sync world); space can bifurcate later if ever needed.
- **PRs are first-class items** with their own identity; review artifacts are per-PR.
- **ItemStore = one identity authority per deployment** (GitHub *or* repo, never mixed) **+ optional repo overlay** keyed by GitHub id for admin/private/large artifacts.
- **Reactor is cloud-only**; **#1 and #4 share one cloud server** (no per-sub-role servers); needs admin accounts/access control.
- **Scenario 3 = `bin/flow auto`** — a serverless, self-capped CLI loop (no local server).
- **Flows and gates are project-owned forge tools** (`tools/flow`→`bin/flow`; gate defs in `project.toml`); Reactor ships SDKs and keeps execution/scheduling/history only.
- **Keep cloud arenas** (mostly implemented; likely the practical way to run cross-platform gates).
- **All repos (promise/flow/forge/reactor) go public together**; submodules + `replace` in the interim.
- Build tooling is the **forge blueprint** (`./make`, `bin/verify`, ratcheted baselines, guard) — not `make.sh`.
