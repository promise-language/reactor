# BASE Engineering — The Project-Facing Layer

> **Status: draft.** A starting point to be reworked, not a finished specification.
>
> Everything here describes what a project adopting Bounded-Autonomy Software Engineering runs
> against — some of it owned and built by the project (gates), some of it project-specific but
> deliberately versioned outside the project's own source (flows). Reactor's half of these
> contracts — how it discovers, schedules, distributes, and executes — is in
> [design.md](design.md). The methodology behind it all is in the [white paper](../WHITEPAPER.md).

## Two layers, often confused

Keeping these apart is the point of this doc:

| | **The BASE layer** (this doc) | **A project's BASE setup** |
|---|---|---|
| What | Reusable, domain-agnostic machinery: the flow common library, the gate SDK, the manifest and envelope contracts, ratcheting baselines, the dev-tooling conventions | One project's *concrete* step composition, item types, prompts, gates, metrics, thresholds, and schedules |
| Owned by | the BASE layer itself — shared across every adopting project | the project |
| Example | step execution, push leases, wire types, "a gate declares metrics with a direction and a mode" | "`promise-tests` emits `test_failures`, enforced, cap 0"; the `implement` prompt template |
| Lives in | a shared repo (see below) | outside the project source — today `workspace/projects/<project>/` |

The flow split is the clearest instance: ~6.8k lines of common library against ~770 lines of
per-project definition. Note that "project-specific" does **not** mean "lives in the project repo" —
per-project flow definitions are deliberately kept outside the project source, for the reason given
under [The principle](#the-principle). The open question is only *which* outside repo holds them,
not whether they move into the project.

Reactor's own BASE setup — the gates and flows *Reactor as a project* runs on itself — is a
project setup like any other, and is not this doc. Reactor's role as the *orchestrator* that
consumes these contracts is [design.md](design.md). Three separate things.

## What lives where

> **Proposed.** This is the layout the constraints point to. The generic/specific boundary is firm;
> the repo names are not.

| Piece | Lives in | Why there |
|---|---|---|
| Reactor server, runner, governor | **reactor** | the orchestrator and its fleet |
| Flow common library, gate SDK, wire types | **BASE layer repo** | reusable, domain-agnostic; the wire types are shared with Reactor |
| Arena provisioning, worktree materialization, flow delivery | **BASE layer repo** | generic machinery (today: workspace) |
| Dev-tooling conventions | **BASE layer repo** | see [promise-forge.md](promise-forge.md) |
| **Per-project flow definitions** — step composition, item types, prompts | **a companion BASE repo, one per project** | project-specific, but must not live in the project tree |
| **Per-project authority config** — roles, step grants, schedules | **the same companion repo** | must be unreachable by the agents it constrains |
| Gate implementations + baselines | **the project repo** | a gate measures the tree, so it comes from the tree |
| Project source | **the project repo** | |

**Today, `workspace` holds three of those rows at once** — the flow common library (`doflow/`,
`wire/`), the delivery and arena machinery, and every project's flow definitions
(`projects/promise/`, `projects/tracker/`). That bundling is the thing this layout untangles, and it
is why "the flows live in workspace" and "the delivery layer is workspace" both sound true today
while pointing at different pieces. In the target state they separate: the common library and the
delivery machinery are generic and stay together in the BASE layer; the per-project definitions move
out to companion repos.

### Why per-project BASE definitions get their own repo

A **companion BASE repo alongside each project** — carrying that project's BASE process definition
and implementation — resolves three things at once that no other arrangement does:

1. **It satisfies the out-of-tree invariant.** Fixing a flow never touches the project worktree, so
   a mid-resolution fix cannot contend with in-flight work ([The principle](#the-principle)).
2. **It keeps the generic layer genuinely generic.** A shared BASE repo accumulating a
   `projects/<name>/` directory per orchestrated project is where the "domain-agnostic" claim leaks
   today. Moving those out means adding a project touches no shared repo at all.
3. **It puts the authority config out of the agents' reach — and this is the strongest reason.**
   Roles and step grants define what an agent may do. If they lived in the project repo, an
   `implement` step could edit its own permissions, and the bound would be self-authorizing: an
   agent that can widen its own grant is not bounded. Flows operate on the *project* worktree, so a
   companion repo is outside their blast radius by construction.

Point 3 also explains why gates can safely live in the project tree while authority cannot. Gates
*are* editable by an implement step — but a weakened gate shows up in the diff, is caught by review
and by ratcheted baselines, and never authorizes anything by itself. A widened capability
authorizes silently and immediately. The two failure modes are not comparable, so the two things get
different homes.

Costs, stated plainly: two repos per project, cross-repo version pinning between a companion repo
and the flow common library, and Reactor needing configuration to find each project's companion.
The pinning is ordinary Promise remote-module resolution, and each companion repo carries
`promise.toml` at its root — so this layout needs no new language feature
([P12](design.md#platform-requirements--requested-of-promise) becomes unnecessary for flows).

### Visibility is a constraint, not a detail

`workspace` and `tracker` are **private**; `reactor`, `promise`, `flow`, and `forge` are public.
That asymmetry constrains the layout directly:

- **A reusable BASE layer has to be public.** Its whole purpose is that other projects adopt it. If
  it stays inside the private `workspace`, the reusability is theoretical. Extracting the generic
  half is therefore not only a tidiness argument — it is what makes the layer usable at all.
- **Companion repos inherit their project's visibility.** A public project's BASE definitions can be
  public; a private project's stay private. Since each companion is independent, this works without
  anything special — which is another point in the companion-repo model's favor, because a single
  shared `projects/<name>/` directory would have to be uniformly one or the other.
- **Nothing public may take a build-time dependency on anything private.** Public Reactor cannot
  depend on private `workspace`.

That last point forces a question worth answering explicitly, and the answer settles more than
visibility.

### Who builds flows

**The companion repo builds its own flow in its own CI and publishes a release. Reactor builds
nothing.**

Reactor ingests the published artifact, verifies it, and serves it onward to runners. That single
choice resolves several problems at once:

- **Visibility stops mattering.** Reactor never needs flow source, so a public Reactor can serve a
  private project's flow without depending on anything private.
- **Reactor stays thin.** Knowing how to build every orchestrated project's flow is the opposite of
  thin, and would drag every project's toolchain into Reactor's release pipeline.
- **The build matrix decentralizes.** Each companion repo runs its own cross-platform CI — which is
  the same native-matrix approach the fleet binaries already use, and it scales by addition rather
  than by Reactor growing a job per project.
- **Ownership follows the code.** Whoever maintains a project's flow owns its build and release,
  without commit access to Reactor.

**Runners still fetch only from Reactor.** Reactor mirrors the release rather than redirecting
runners to it, which preserves the
[outbound-to-Reactor-only invariant](design.md#deployment-topology--server-governor-runner): an
arena with tightly restricted egress needs no path to a code-hosting site, and there is one trust
path to verify instead of two. Verification happens once, when Reactor ingests the release.

### One binary per project

**A project needs exactly one flow binary.** A flow is self-describing, so a single binary can carry
the resolution logic for every item type the project defines, and every step — including steps that
only certain roles may run. The distribution key is therefore `(project, os, arch)`, not
`(project, flow, os, arch)`: one artifact per project per platform, and the build matrix is
platforms only.

**One binary does not weaken the authority model**, which is the natural worry — shouldn't an
untrusted step get a smaller binary containing less dangerous code? No, and the reason is the
model's central premise: **capability comes from the environment, not from what code is present.**
The flow is never trusted to limit itself, so what it *could* do given credentials it does not have
is irrelevant. A binary containing merge logic, invoked for a `plan` step, cannot merge — it holds
no credential that would let it, and the API would reject the call. Splitting binaries to
constrain behavior would be defending with the one mechanism the design explicitly does not rely
on.

### Naming the shared layer

**A BASE layer already exists in embryo — it is
`workspace`** (still private), which delivers flows, provisions
arenas, and materializes worktrees. It is not framed as such, and it carries exactly the two-layer
mixture described above: generic machinery beside `projects/promise/` and `projects/tracker/`. The
question is less "create a base repo" than "name the layer that exists, move the per-project halves
out, and port it to Promise" — which follows from the flow common library being Promise.

Candidate consolidation: **workspace** (delivery, provisioning, arena setup) +
[forge](https://github.com/promise-language/forge) (dev-tooling conventions) + the flow common
library and gate SDK. The [white paper](../WHITEPAPER.md) would move too — the methodology is not
the orchestrator — at the cost of breaking public inbound links from promise's README and the
generated `promise-lang.org/base` page.

## Dev tooling

How a project builds and runs its own tools is part of this layer. The Go
[forge](https://github.com/promise-language/forge) blueprint (`./make` → `bin/`, source-hash
staleness checks, committed trampolines) is what BASE uses today; most of that machinery exists
only to work around `go run`, and stops being necessary once tools are written in Promise. See
[promise-forge.md](promise-forge.md).

## The principle

**Push domain logic to the project; keep Reactor thin.** Reactor owns scheduling, execution, state,
and history. It never owns flow logic, gate definitions, or metric semantics.

But "the project owns it" splits differently for gates than for flows, and both halves fall out of
one distinction — *what each does to the worktree*:

> **A gate measures the tree, so it must come from the tree.**
> **A flow modifies the tree, so it must come from outside the tree.**

A gate defines what "correct" means for the tree it runs against. Built from anywhere else, it could
disagree with the code it is gating, and the quality floor would be measuring the wrong thing.

A flow is the thing *editing* the worktree, and **the thing modifying a worktree cannot be versioned
inside it.** If flow source lived in the project repo, fixing a flow bug mid-resolution would mean
fetching and rebasing onto a worktree that already carries substantial in-progress changes — the
flow fix and the item resolution then contend over the same tree, each undoing or blocking the
other. That cost is worst exactly when it is least affordable: flow bugs are common while
bootstrapping a project, which is when resolution is most fragile and worktrees are most often
mid-change.

Keeping flows **project-specific but defined outside the project source** removes the contention
entirely. A flow fix or a new flow feature is authored asynchronously, touching no in-flight
worktree, and the running resolutions pick the update up for their *remaining* steps.

This is also why flows are delivered as **binaries fetched from Reactor** rather than built from
source in the worktree: building in-tree would reintroduce the very contention the split exists to
avoid.

- **Gates: source lives with the project, and is always run from source.** A gate is a project dev
  tool — the promise repo builds its own `bin/gate` from `tools/build/cmd/gate/` and keeps its
  baselines under `tools/gates/`. Gates are **never distributed as prebuilt binaries**: the runner
  builds (or directly runs) the gate from the checked-out tree before executing it, so the gate and
  the code it measures always come from the same commit. The project exposes its gate registry
  through a discovery command; Reactor reads it and keeps only *scheduling*, *execution*, and
  *history*, plus deployment-side concerns like arena assignment.
- **Flows: delivered as prebuilt binaries by Reactor.** A flow is **mostly common library, with a
  thin project-specific head.** The shape is visible in today's Go implementation in the workspace
  repo: `doflow/` and `wire/` hold the reusable machinery — the app skeleton, step execution,
  commit-message handling, push-lease logic, artifact extraction, the wire types — while
  `projects/<project>/do/` holds what is actually specific to one project: **the step composition,
  the item types, and the prompts**. The proportions matter: roughly 6.8k lines of shared machinery
  against ~770 lines of per-project definition including templates. The Promise implementation keeps
  that structure — a common-library module plus a thin per-project head — and gains one thing from
  the language unification: the wire types become a module **shared with Reactor** rather than
  a second copy kept in sync by hand.

  One binary serves a whole project — see [One binary per project](#one-binary-per-project) — built
  by the companion repo's CI per `(project, os, arch)` and served by the Reactor server alongside
  the governor and runner builds it already distributes. The runner resolves which project it is
  working on and fetches that project's binary for its platform. The project repo itself carries no
  flow implementation and no prompt templates.

  One property of the installed file is load-bearing: **it IS the flow binary** — no launcher, no
  wrapper script — so a caller waits on a single process and a kill targets it directly. Whatever
  delivers it must preserve that.

  This is the same distribution channel as the fleet's own binaries, which is the point: one build
  matrix, one signed artifact path, one self-update story, and a flow that can be updated without a
  commit to either the project repo or the arena image.

  **The flow version is resolved per step, not pinned per item.** That is what makes an async fix
  useful: a flow bug found while item N is mid-resolution is fixed outside, published, and picked up
  by that same item's *remaining* steps — no restart, no rebase, no touching the worktree. The
  tradeoff is deliberate and worth stating plainly: an item can be resolved partly by one flow
  version and partly by the next, so a resolution is not reproducible from a single flow version.
  Recovering from a broken flow immediately is worth more than that reproducibility, especially
  during project bootstrap when flow bugs are frequent.

  Every actor fetches flows the same way, from the same server — including external contributors,
  who have an account and a restricted role rather than a serverless path of their own. That
  uniformity is a direct benefit of dropping the
  [serverless variant](design.md#no-serverless-variant): one distribution channel, one set of
  artifacts, no second story to keep working.

A flow remains self-describing regardless of who ships it — it declares its own item types and
eligibility, and carries **no Reactor configuration**.

### Language

**Flows are Promise, always** — they are part of the BASE implementation, so there is no reason for
them to be anything else. **Gates are Promise for Reactor's own project, and whatever the project
uses for everyone else.** See [design.md](design.md#language).

That asymmetry is not an inconsistency; it is the system's one deliberate polyglot boundary. BASE
must be able to orchestrate a project it shares no runtime with, and the gate — the piece that
belongs to the project and is built from the project's own tree — is exactly where another language
legitimately enters. Which is why the gate contract is a manifest plus a JSON envelope over a
subprocess and **not** an SDK interface: a project must be able to satisfy it by printing JSON, with
no BASE library, no Promise, and no code generation. A Promise gate SDK exists for convenience and
Reactor's own gates use it, but any gate that depends on the SDK's *existence* has broken the
contract.

The delivery split above is language-independent, so neither side changes shape. What changes is
what each side needs from the platform.

**Flows — nothing new.** They stay prebuilt binaries served per `(project, os, arch)`, built by the
companion repo's own CI: a native platform matrix today, one job once `--target` cross-compilation
lands. Because the artifact is a real binary, the no-launcher property holds for free.

**Gates — this is where `promise run` matters, for Promise-based gates.** A gate must come from the
tree under test, so it is built or run from source in the worktree on every execution. For a Promise
project that makes the [Promise tooling model](promise-forge.md) load-bearing rather than a
convenience, and it puts three requirements on it. (A gate in another language faces the same three
questions in its own toolchain — they are properties of "build from the tree on every run", not of
Promise.)

1. **Compile caching must be real**, or every gate run pays a full compile of the gate before doing
   any work. In a fresh ephemeral arena the cache is cold by definition, so the arena image should
   ship or mount a warm cache — arena-provisioning work, not language work.
2. **`promise run` must *exec* the compiled binary, not stay resident as its parent.** The runner
   waits on the gate process and may kill it on timeout; an interposed parent turns signal delivery
   and exit-status propagation into a wrapper problem.
3. **Stdout must belong exclusively to the gate.** Reactor parses the gate's stdout as a JSON
   envelope, so no compiler diagnostic, progress line, or cache message may ever land there —
   stderr only.

Requirement 3 is specific to gates and does not arise for flows, which is worth noting because it
is invisible until a first compile warning silently corrupts a gate result.

## An example setup: an OSS project

> **This is one BASE setup, not the architecture.** Reactor resolves work items of any kind, and
> *who may do what* is configured through
> [roles and step grants](design.md#authority-roles-steps-and-capabilities). What follows is how
> those primitives get arranged for a public open-source project — useful as a worked example, and
> nothing Reactor should be designed around.

Four arrangements of the same machinery, distinguished only by which role is acting and which flow
runs — not by different deployments. All four talk to the same cloud Reactor; there is
[no serverless variant](design.md#no-serverless-variant).

| Arrangement | Role | What it does |
|---|---|---|
| **Production line** | admin | Drains a backlog across the arena farm in a conflict-avoiding order, with scheduled gates. |
| **Manual contributor** | contributor | Resolves one claimed issue and opens a PR. Cannot merge. Gates run in their own worktree. |
| **Auto loop** | contributor | The same, unattended, until a stop condition — quota, cost cap, or an empty queue. Self-capped. |
| **PR intake** | maintainer / admin | Runs review and security flows on PR items plus cross-platform gates; merges or returns to sender. |

The contributor arrangements differ from the admin one in **role**, not in infrastructure: a
contributor has an account whose ceiling permits reading everything and producing a PR, and permits
nothing else. Their limits are enforced by that ceiling rather than by their own good behavior,
which is the whole point of moving this out of "scenario" and into "configuration".

Claim coordination is GitHub assignee plus the lease ledger; the item source of truth is GitHub
issues throughout.

## Single-issue work, first-class PRs

```
issue (work definition, flow:resolve)
   ├─ contributor A: resolve ─▶ PR #a  (first-class item; its own review artifacts)
   └─ contributor B: resolve ─▶ PR #b  (separate identity; separate review)

each PR ──admin review/security (flow:review)──▶ cross-platform gates ──▶
     merge  OR  return-to-sender (PR/issue back to open + notes)
```

Admin review is **more flows run on the PR item**; artifacts attach to the **PR's identity** — on
GitHub when public, in the private overlay otherwise. Eligibility routing uses `flow:<binary>`
labels plus assignee: contributors run `flow:resolve`; admins run `flow:review` and `flow:gate`.

Untrusted work is **bracketed by trusted gates**: a less-trusted role runs every step *except*
pushing to origin (it produces a PR), and a trusted review either merges it, returns it to sender,
or escalates to the human at the top of the trust ladder.

## Gate discovery — the project declares, Reactor schedules

Tracker required each gate to be entered by hand into server config (name, command, schedule, host
filter, metric directions, ratchet caps). That doesn't scale to a multi-project Reactor and forces
a maintainer to mirror project knowledge into the server. The relationship is inverted here: **the
project declares its gates; Reactor discovers them.**

**The contract.** A project exposes a single command — convention: `bin/gate list --json` — that
emits a manifest describing every gate it offers plus a global preflight command. The manifest is
the source of truth for gate *identity*, *runtime*, *eligibility*, and *metric semantics*.

### Manifest shape (v1, JSON)

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

| Field | Meaning |
|---|---|
| `schema_version` | Major version; Reactor refuses unknown majors. |
| `preflight` | Optional global setup command Reactor runs after a fresh checkout, before any gate (build the gate binary itself, sync submodules, sanity-check the tree). OS-dispatched. |
| `gates[].name` | Stable id; keys metric history and baselines. **Must be unique within the manifest.** |
| `gates[].command` | Exec line. OS-dispatched. |
| `gates[].host_os` | `linux` / `darwin` / `windows` / `any`. Eligibility filter. |
| `gates[].host_arch` | Optional `amd64` / `arm64` filter — lets a project target "linux arm64" separately from "linux amd64" without a target-triple grammar. Omitted ≡ any. |
| `gates[].timeout` | Duration (`30m`, `2h`). |
| `gates[].schedule` | `every <dur>`, `daily`, `weekly`, `after-every-commit`, `manual`. |
| `gates[].allow_dirty_tree` | Skip the post-run clean-tree check. |
| `gates[].tags` | Free-form; attached to auto-filed bugs. |
| `gates[].metrics[]` | One spec per metric the gate emits. |

**OS-dispatched commands.** `preflight` and `gates[].command` each accept either a **string** (used
on every OS) or an **object** `{ "default": …, "linux": …, "darwin": …, "windows": … }` — the
host-OS key wins, `default` is the fallback. OS keys use the same vocabulary as `host_os`. A bare
string is shorthand for `{ "default": … }`.

**Metric spec** — `{name, type, direction, mode, cap?}`:

- `type`: `int` / `float` / `bool` (bool persisted as 0/1; direction must be `down` for "has-X"
  invariants).
- `direction`: `up` (higher is better) or `down` (lower is better).
- `mode`: `enforced` (a regression fails the gate), `pending` (recorded, doesn't fail), or
  `informational` (recorded only — never causes a regression).
- `cap` (optional): a direction-aware ceiling or floor at which baseline auto-ratcheting stops.
  Prevents a one-time fluke — coverage spiking on a partial run, say — from making every future run
  "regress". `up` → baseline ≤ cap; `down` → baseline ≥ cap.

### Gate output envelope

Every gate writes a `GateOutput` JSON object to stdout, with human-readable progress on stderr. The
envelope is **mandatory** — there is no exit-code-only mode, a deliberate simplification. It
carries the target (one gate run = one target, invariant), a flat `metrics` map keyed by the names
declared in the manifest, optional per-file test groups for granular history, and a `complete`
marker. Reactor consumes the envelope and never parses a gate's human-readable output.

The authoritative schema belongs alongside the gate SDK.

### Discovery lifecycle

1. **Adopt.** The first time a worktree is registered, Reactor runs `preflight` for the host OS,
   then the manifest command, validates the result, and creates a gate record per entry — merging
   any deployment-side overrides already stored under that name.
2. **Refresh.** On each new commit, and on a slow tick, Reactor re-runs the manifest command. New
   gates adopt with defaults. Removed gates retire — history preserved, scheduling stopped. Changed
   `direction` or `type` is flagged for admin review, because changing metric semantics mid-history
   would silently invalidate every baseline behind it.
3. **Execute.** Reactor's scheduler picks eligible gates by host OS × arch × deployment overrides,
   runs `preflight` if the worktree is fresh, runs the gate command, parses the envelope, and
   writes results to its ledger.

## No manual gate registration

Gates exist only by discovery. There is no path that creates one by hand in the server, from the UI
or otherwise — a gate that Reactor knows about but the project does not declare would be exactly the
mirrored-project-knowledge problem discovery exists to remove. Deployment-side overrides — arena
assignment, manual narrows, exceptions — are the one thing an operator sets directly, and they only
ever constrain or annotate what the project already declared.

*(This is a lesson carried from prior art, not a data migration — see
[design.md](design.md#context).)*
