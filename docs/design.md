# Reactor — Design

> Reactor's architecture: the implementation language, the seams, the authority model, the
> deployment topology, and the pluggable persistence split
> (`ItemStore` / `ConfigStore` / `LedgerStore`). Reactor is written in **Promise**.
>
> The BASE layer — flows, the gate manifest contract, and the engineering-process setup a project
> adopts — lives in [base-engineering.md](base-engineering.md); how Promise-based dev tools are
> built and run is in [promise-forge.md](promise-forge.md). This doc covers only what Reactor
> itself is and does.

## Context

Reactor is the open-source orchestrator for Bounded-Autonomy Software Engineering —
`github.com/promise-language/reactor`, dual Apache/MIT, sibling to `promise`, `flow`, and `forge`.
Those four are **public**, so dependencies among them are ordinary versioned dependencies — no
submodules, no `replace` directives.

Two repos in the picture are **private**: `workspace` (delivery and arena provisioning — see
[Deployment topology](#deployment-topology--server-governor-runner)) and `tracker` (the predecessor
it serves). Nothing public may take a build-time dependency on either, which is a live constraint on
where the BASE layer lands — see [What lives where](base-engineering.md#what-lives-where).

### Objectives

Two, and every decision here should be judged against them.

**1. A clean, reusable BASE implementation that applies to many projects.** Reactor is a **new
project, not a migration**: the private `tracker` is prior art whose topology, UI, and hard-won
operational lessons inform the design, but **no compatibility with it is required** — not on disk,
not in its APIs, not in its data. Moving an existing hand-built BASE process onto Reactor is a
later, secondary concern, and no part of the design should be bent to make it easier.

**2. Runs reliably unattended for prolonged periods.** Not "works when demonstrated" — *keeps*
working across crashes, network partitions, exhausted quotas, hung agents, machines that reboot,
and flows that turn out to be buggy, with nobody watching. This is a design constraint, not an
operational aspiration: it is why the runner reconnects outbound instead of being reachable, why the
governor supervises the runner, why leases are reclaimable, why interrupted work resumes rather than
restarts, why quota exhaustion pauses instead of crashing, and why a flow can be fixed and picked up
mid-resolution.

**The two objectives meet in the authority model.** When a person is watching, a step that does
something it shouldn't gets caught. Unattended, the only thing between a mistake and damage is what
the agent was *able* to do. So "runs unattended for long periods" is exactly what makes
[role ∩ step](#authority-roles-steps-and-capabilities) non-negotiable rather than a nicety — the
guardrails are load-bearing precisely because nobody is at the wheel.

### Shape

Reactor carries forward the proven shape of its prior art — the admin web UI, the runner/governor
topology — as *design*, and re-derives everything else. It is written in Promise, and its
persistence is split by concern rather than sitting behind one repo-backed store.

**Push domain logic out of Reactor, keep Reactor thin.**
Reactor owns *scheduling*, *execution*, *state*, and *history* — never flow logic, gate definitions,
or metric semantics. Where that logic lives splits by what it does to the worktree: **gates measure
the tree and so come from the tree** (project-owned, built from the commit under test); **flows
modify the tree and so come from outside it** (project-*specific* but versioned independently, and
distributed by Reactor as binaries). The reasoning is in
[base-engineering.md](base-engineering.md#the-principle).

### Language

**BASE is implemented in Promise, end to end** — the Reactor server, the runner and governor, the
flow common library, every per-project flow definition, and the delivery/arena layer. There is no
per-component language choice to make, so there is no table of them; *where* each component lives is
the interesting question, and that is
[What lives where](base-engineering.md#what-lives-where).

Exactly two things are not Promise:

| | Language | Why |
|---|---|---|
| **Another project's gates** | any language, or none | the one deliberate polyglot boundary |
| **forge dev tooling** | Go today → likely nothing at all | scaffolding around Go's limits; see [promise-forge.md](promise-forge.md) |

**The gate exception is deliberate.** A gate belongs to the project it measures, and that project
may be written in anything — so the gate contract is a **manifest plus a JSON envelope over a
subprocess**, satisfiable in any language with no SDK at all. Reactor ships a Promise gate SDK for
convenience and Reactor's own gates use it (Reactor is a project too, and it dogfoods), but any gate
that depends on the SDK's *existence* has broken the contract. Keeping that boundary
language-agnostic is what lets BASE orchestrate a project it shares no runtime with.

Reactor is the first large **application** written in Promise — the compiler proves agents can build
a systems project *in Go*; Reactor is the test of whether they can build a real, long-lived service
*in Promise*. Gaps it hits are platform work, not Reactor workarounds: see
[Platform requirements](#platform-requirements--requested-of-promise).

## Seams are process boundaries — by design, not by accident

BASE is one language throughout, so nothing *forces* these components apart — they could be linked
into one binary. They are kept as separate processes on purpose, and each boundary earns its place
twice over: once for a reason specific to it, and once for reliability, which applies to all four.

| Seam | Boundary | Why it stays a boundary |
|---|---|---|
| flow ↔ Reactor | Reactor HTTP API | **Authority.** Every item mutation must cross a point where the actor's role and the step's grant can be checked. A linked library has no such point. Flows also run in arenas, on other machines. |
| runner ↔ flow | subprocess | **Authority again.** The runner assembles the step's environment and withholds credentials the step may not use; a sandboxed child process is where that is enforceable. |
| server ↔ runner | HTTP long-poll | Physically different machines, and the [outbound-only invariant](#deployment-topology--server-governor-runner). |
| Reactor ↔ gates | subprocess + JSON envelope | **Language independence.** A gate may be written in anything; this is the boundary that makes that true. |

The shared reason is reliability. A process is the unit the operating system will actually enforce
things about, and [unattended operation](#objectives) needs exactly those guarantees:

- **Fault isolation.** A flow that segfaults, exhausts memory, or corrupts its own state takes down
  one process. It cannot damage the runner supervising it or the server holding everyone's state —
  the only channel out is an API call that gets validated.
- **Resource bounds that are real.** Timeouts, memory caps, and CPU limits are enforceable on a
  child process and merely advisory inside one. A gate with a 30-minute limit is *stopped* at 30
  minutes.
- **Killability.** A hung agent must be terminable from outside, by something that is not itself
  hung. That requires a separate process — and it is why the delivered flow must be the real binary
  with no wrapper, so a kill lands on the flow rather than on a launcher.
- **Supervision and restart.** Governor restarts runner, runner retries steps. A supervision tree
  only exists across process boundaries; in-process, a crash is a crash.
- **Independent lifecycle.** The runner self-updates while the server keeps serving; a flow is fixed
  and picked up mid-resolution. Coupled in one process, every upgrade is a coordinated outage.

An in-process design would have to reimplement all of this badly, and would still lose to a null
pointer. Paying a serialization cost to get it from the OS is a good trade.

Consequences:

- **The flow↔Reactor contract is specified, versioned, and conformance-tested** as an API rather
  than compile-checked. That cost is now paid deliberately, in exchange for an enforcement point —
  not accepted because two languages could not link.
- **The wire types can be a shared Promise module.** Both sides speak the same language, so the
  protocol gets one definition used by server and flow client alike, instead of two hand-kept-in-sync
  copies. That module needs a home, which is where the
  [BASE-layer repo question](base-engineering.md) and
  [P12](#platform-requirements--requested-of-promise) meet.
- **The GitHub client is written once, in Reactor.** With
  [no serverless variant](#no-serverless-variant), flows never talk to GitHub directly — they go
  through the Reactor API, and Reactor owns the only GitHub client.
- **Nothing is constrained by an existing on-disk format.** Persistence shapes are chosen for
  clarity and for the conformance suite. There is no predecessor layout to match.

The no-FFI fact still binds wherever another language legitimately remains — a project's own gates,
and whatever Go tooling survives.

## Authority: roles, steps, and capabilities

> **Status: proposed model, not yet settled.** Reactor resolves work items of any kind, and *who may
> do what* is configuration rather than a built-in scenario. One concrete arrangement — how these
> primitives get configured for a public OSS project — is worked through in
> [base-engineering.md](base-engineering.md) as an example.

Autonomy is bounded by what an agent is *able* to do, not by what it is asked to do. Reactor makes
that mechanical with two independent grants that compose.

**Role — the principal's ceiling.** A role is what an actor may ever touch: an admin reads and
modifies everything; a contributor reads everything but can only produce a PR, never merge it; an
observer reads and changes nothing.

**Step — the task's grant.** Every step of every flow declares which roles may resolve it *and*
what that step may do while resolving it — independent of who is running it.

> **Effective authority = role ∩ step.** Neither can widen the other. A step never gains reach
> because an admin ran it, and a role never gains reach because a step needed it.

That intersection is the whole model. It applies least privilege twice: once to the actor, once to
the work.

### Why the step grant matters even for a fully trusted actor

An admin running a `plan` step should still be unable to modify source, because *planning is not
editing* — the constraint describes the task, not distrust of the person. This is what makes the
model useful beyond access control: it turns "this step should only do X" from a prompt instruction
an agent may ignore into a boundary it cannot cross. Illustrating with the steps of a resolution
flow:

| Step | May read | May write | Notably cannot |
|---|---|---|---|
| `plan` | item, tree, gate history | the plan, onto the item | touch the tree at all |
| `implement` | everything in scope | the tree | commit, or alter the item beyond progress |
| `inspect` | everything in scope | the inspection, onto the item | modify the tree or item state |
| `integrate` | everything in scope | the tree, and a commit on the work branch | push to origin |
| `publish` | the tree, the item | a PR | merge it |
| `review` | everything | verdict onto the item; merge | — (gated by role, not step) |

The dangerous verbs — commit, push, merge — are isolated into their own steps, so each can be
assigned to a different trust level. The white paper's "untrusted work is bracketed by trusted
gates" stops being a convention and becomes a property the system enforces.

**Why `integrate` and not `commit`.** A step that only records what `implement` produced would be
tidier, but it does not survive contact with a moving trunk. By the time work is ready to land,
other changes may have landed ahead of it, and reconciling with them can require resolving conflicts
and genuinely adapting the implementation to fit what changed. That is creative work, not a
mechanical commit — so the step that lands a change needs **tree write**, and pretending otherwise
would just mean the grant is wrong whenever a rebase is non-trivial.

Naming it `integrate` says the real thing: **every landing is an integration, and a clean
fast-forward is the degenerate case.** Modelling the easy path as the rule and conflicts as an
exception gets the capability wrong in exactly the situation where being wrong matters most.

Two things keep that grant from swallowing the model. Integration is bounded *after* the fact, by
gates: a botched conflict resolution that drops someone else's change has to survive the same
mechanical quality floor as any other tree state, which is precisely what ratcheted baselines are
for. And it is bounded *before* the fact by the role — an actor who may integrate still cannot push
to origin or merge the PR, so the trust ladder is intact.

**This table is illustrative, not normative.** Reactor defines the capability *vocabulary* and
enforces the intersection; the step set and its grants are project-specific and live in the
project's companion repo ([What lives where](base-engineering.md#what-lives-where)). A project that
wants a separate narrow `commit` for the clean path and a distinct `resolve-conflicts` step for the
messy one can declare exactly that — the model does not care how finely the work is sliced, only
that each slice states what it may touch.

### Where it is enforced

A capability model that only the flow honors is documentation, since the flow is the thing being
constrained and an agent drives it. So the grant is a **specification**, and enforcement is a
separate, plural concern: many choke points, at different layers, and no single one is the
mechanism.

> **The test is materiality, not immediacy.** A restriction counts as enforced if a violation is
> **prevented**, or **detected and undone** — synchronously or after the fact. What breaks the
> premise is a violation that silently stands.

Choke points available, roughly outermost to innermost:

| Layer | Mechanism | Naturally enforces |
|---|---|---|
| Environment | **Credential scoping** — the step is never handed a key it may not use | push, merge, secret access, model quota |
| Environment | **Sandbox** — filesystem and network confinement; a read-only worktree mount | tree read vs. write, egress |
| Agent harness | **Pre-tool hooks** — intercept the agent's tool call before it runs | fine-grained: edits to a path, specific commands |
| Agent harness | **Tool availability** — never expose the tool at all | a `plan` step with no edit tool |
| VCS | **Pre-commit / pre-push hooks; server-side branch protection** | commit shape, what may reach which branch |
| Reactor API | **Per-call validation** against role ∩ step | every item mutation |
| Post-hoc | **Diff and audit review** — did the step touch anything outside its grant? | anything the layers above missed |

Two of these deserve emphasis. **Credential scoping is the strongest** because it is not a check
that can be bypassed — you cannot do what you have no key for. And **API-side validation is why the
[flow↔Reactor wire seam](#seams-are-process-boundaries--by-design-not-by-accident) is a security
asset rather than only a decoupling one**: were flows linked in-process, item mutations would have
no boundary to cross and there would be nothing to validate.

**Preventive and detective both count.** A pre-commit hook stops a violation; a post-hoc diff audit
that rejects the step's output, reverts the tree, and escalates is slower but equally material.
Some capabilities have no clean preventive choke point and are only checkable after the fact — that
is acceptable, and far better than the alternative of quietly not enforcing them.

**But a grant with no choke point behind it is advisory, and should be labelled as such.** Because
grants are declared data, Reactor can report which restrictions are materially enforced and which
are merely stated — and that distinction should be visible, not glossed. A model that claims uniform
enforcement it does not have is worse than one that is honest about its edges.

A second property falls out of the same declarations: **Reactor can check statically that a role is
capable of completing a flow** — every step's required capabilities must lie within the role's
ceiling — and reject an impossible assignment before dispatching any work.

### Open — the capability vocabulary

The model is only as good as the resource/verb vocabulary it is expressed in, and that is the part
still to nail down. A starting proposal, to be argued with:

| Resource | Capabilities |
|---|---|
| Item | read · create · annotate:`<kind>` (plan, inspection, review, note) · state (open/close/reassign) · artifact write |
| Source tree | read · write |
| VCS | commit · push:branch · push:origin · pr.create · pr.merge |
| Gates | run · results.read · baseline.write · exception.grant |
| Orchestration | item.claim · step.dispatch · arena.provision |
| Deployment | config.read · config.write · secret:`<name>` |

Questions this has to answer before it is settled: whether `annotate:<kind>` is the right
granularity or item fields need per-field grants; whether tree write should be path-scoped (a step
that may edit source but not `.github/`); how a step declares a capability it needs *conditionally*;
and whether roles compose (inherit) or are flat.

**Where the declarations live is part of the model, not a packaging detail.** Roles and step grants
must sit somewhere the agents they constrain cannot reach — otherwise an `implement` step could
widen its own grant, and the bound would be self-authorizing. Since flows operate on the *project*
worktree, the declarations belong outside it: see
[What lives where](base-engineering.md#what-lives-where).

## Persistence

All three stores ride one **minimal record core** — `Get` / `Put` / `Delete` / `List(ns)` plus
`Filter(ns, pred)` and `Search(ns, q)` — so backends stay swappable and a conformance suite can
exercise every implementation identically.

### ItemStore — composite identity (GitHub) + private overlay

GitHub is the single **identity** authority; a private overlay is keyed by that identity — *not*
two competing populations to sync, which would inevitably leak and mix. One item, loaded by
merging layers:

- **Identity + public state = GitHub issues/PRs** (the source of truth, visible to everyone).
  Issues are work definitions. **PRs are first-class items with their own identity** (their PR
  number), because multiple contributors may produce different PRs for one issue and a security
  review of PR-A must **not** apply to PR-B. Public fields map to labels/assignee/state/body;
  small public artifacts to an orphan artifacts branch.
- **Private overlay = `RepoItemStore` (or KV), keyed by the GitHub id.** Holds admin-only and
  large artifacts that shouldn't or can't live on a public issue (the project can't host large
  capacity for everyone, and GitHub issues can't be deleted). Lives with the admin cloud Reactor.

Implementations:

- **`GitHubItemStore`** — issues + first-class PRs, implemented natively in Promise over the
  `http` client (see [Platform requirements](#platform-requirements--requested-of-promise)).
- **`RepoItemStore`** — one JSON record per item in the repo; used **as the overlay keyed by
  GitHub id** on a GitHub deployment, **and** as the standalone identity store for GitHub-free
  (private/offline) deployments. The on-disk shape is ours to choose.
- **`CompositeItemStore`** — merges GitHub identity + overlay on load; what the admin Reactor uses.

**One identity authority per deployment — GitHub *or* repo, never both for the same items** — so
nothing leaks or mixes across worlds. The repo overlay only ever *adds* private artifacts to a
GitHub-identity deployment; it is never a second source of item identity.

### ConfigStore — the deployment owner's residual

Deliberately minimal: only things the project *can't* own because they are the **deployment
owner's** choice — quota and cost limits, model credentials, arena allocation and provider creds,
admin access control. Flows and gates are **not** here; they live in the project.

### LedgerStore — per-server active state

Lease ledger, gate run history and baselines, orchestration/scheduler run state, turn registry,
quota snapshot, notifications, the GitHub read-index cache. CRUD-shaped and hot. Implementations:
repo-backed (`_*.json`) and a KV example.

## No serverless variant

> **Status: proposed.** A Reactor server is always in the picture. There is no mode in which a
> contributor clones a project, runs a flow against GitHub directly, and never touches a server.

Supporting both a server-centralized and a serverless path means two backends, two claim-coordination
schemes, two conformance surfaces, and two sets of bugs, forever. But the decisive argument is not
cost, it is that **the [authority model](#authority-roles-steps-and-capabilities) cannot be enforced
without a server.** With no Reactor there is no API to check a mutation against, and the credentials
live on the machine being constrained — every grant collapses into an instruction the flow is
politely asked to honor. A serverless contributor is not running BASE with weaker enforcement; they
are running an unbounded agent under the same name. Shipping that as a supported mode would
undercut the thing the system exists to provide.

**The contributor scenario survives as a role, not as a deployment mode.** An external contributor
gets an account on the project's Reactor with a role that reads everything and can produce a PR but
never merge one. The on-ramp is preserved — clone, resolve, open a PR — and it is now enforced
rather than trusted. The server is cloud-hosted regardless, so the contributor needs an account,
not infrastructure.

What this costs: a project cannot be adopted with zero server, and a contributor cannot work fully
offline. Reintroducing a serverless mode later means answering "what enforces the bound when
nothing is watching" — worth revisiting only with an answer to that.

## Deployment topology — server, governor, runner

Reactor is **not one process.** The server is cloud-hosted; the work happens in a workspace on
some other machine or container entirely. Three roles, three deployables:

```
                          cloud
                ┌─────────────────────────┐
                │     Reactor server      │  state · scheduling · admin UI
                │       bin/reactor       │  flow API · binary distribution
                └────────────▲────────────┘
                             │  HTTPS, always initiated by the runner (long-poll)
        ┌────────────────────┼────────────────────┐
        │                    │                    │
   ┌────┴─────┐         ┌────┴─────┐         ┌────┴─────┐
   │ governor │         │ governor │         │ governor │   supervises · swaps binaries
   │  runner  │         │  runner  │         │  runner  │   executes work in a worktree
   └──────────┘         └──────────┘         └──────────┘
    bare metal            container            cloud VM
   (a dev machine)     (ephemeral arena)   (ephemeral arena)
```

**The invariant: the server never reaches into a host.** Every runner opens its own outbound
connection and long-polls for work. That is what lets runners live on personal machines behind
NAT, inside containers, and on ephemeral cloud VMs without inbound firewall holes, SSH
credentials, or out-of-band reachability for every host. Nothing in the design may quietly
introduce a server→host connection.

**Server** (`bin/reactor`, cloud). Holds all state, makes all dispatch decisions, and exposes:

- **Admin web UI** — assets compiled into the binary via `` `embed ``.
- **Flow API** — the wire contract flows speak: claim / release, load state, resolve
  artifact, worktree coordination. This is the seam described
  [above](#seams-are-process-boundaries--by-design-not-by-accident), and the point where
  [authority](#authority-roles-steps-and-capabilities) is checked.
- **Runner API** — registration, the long-poll action channel, and output streaming.
- **Binary distribution** — how a host bootstraps, how the fleet self-updates, and how a worktree
  gets the flow it will run. Reactor is a **registry, not a build system**: it serves artifacts
  built elsewhere and never needs their source
  ([why](base-engineering.md#visibility-is-a-constraint-not-a-detail)). Two classes, and they key
  differently:
  - **Fleet binaries** (governor, runner) — keyed by `(os, arch)`. Generic: one set serves every
    project in the deployment.
  - **Flow binaries** — keyed by `(project, flow, os, arch)`. **Flows are project-specific**, so
    this set multiplies with the number of projects Reactor orchestrates, and the runner must
    resolve *which* project's flow it needs before fetching (it already identifies the project
    from the worktree's origin, and refuses to guess when it cannot). The runner resolves the flow
    version **per step rather than per item**, so a flow fixed mid-resolution is picked up by the
    remaining steps of work already in flight — see
    [base-engineering.md](base-engineering.md#the-principle) for why that matters.
- **Ingress for GitHub events** — PR-open and related signals routed to the scheduler.

**Runner** (`bin/runner`, one per workspace). Long-lived. Registers with the server advertising its
host OS, arch, role, and capabilities, then long-polls for actions: run a flow binary, run a gate,
prepare a worktree, provision an arena (in the arena-host role). Streams output back as it goes.
**The runner self-updates** — that is what makes shipping new runner code to a deployed fleet
automatic after a server upgrade, with no operator work per host.

**Governor** (`bin/governor`, one per host/arena). A minimal supervisor: fetch the runner binary
on first start, keep it alive, and swap it when an update is staged (a distinguished runner exit
code means "update staged — swap and restart"; a crash restarts with backoff, and auto-rolls-back
if the crash follows an update). It knows nothing about items, gates, or arenas. It is
operator-installed and does **not** self-update, which is precisely why it must stay small and
change almost never.

**Cloud arena providers are the exception** to the outbound rule: provisioning a GCP or AWS VM is a
sequence of authenticated API calls with no host to run an arena-host runner on, so those execute
in the server process. Local providers (Docker, Tart, Hyper-V, WSL2) run inside an arena-host
runner on the machine that hosts them.

**Worktree materialization is delegated**, not built into the runner. Before spawning a flow
binary, the runner clones or refreshes the
`workspace` repo (still private) and runs its setup tool, which
reads the arena context from the runner over loopback, materializes the project working tree from
the read-only bare repo mounted into the arena (via `--local --shared` alternates rather than a
full remote clone), repoints `origin` at the real upstream, and provisions dependencies keyed off
the arena's purpose. `setup` only sets up — it never runs a gate or a flow; the runner drives all
real work afterward, so one tool serves both ephemeral and persistent arenas. This layer is Go
today and moves to Promise with the rest of BASE; either way it is invoked as a subprocess, so its
language costs Reactor nothing.

### What the split costs in Promise

The topology follows tracker's, which is proven in production. Three of its properties turn into
hard platform dependencies in Promise:

1. **The TLS client blocks the entire fleet, not just GitHub.** Every runner and every governor
   talks to the cloud server over HTTPS. Without P1's client half there is no fleet at all — this
   is why P1 blocks the engine itself, not merely GitHub access.
2. **A serial HTTP server is incompatible with long-polling.** N runners each hold a connection
   open for the full poll interval. With `http.Server.serve` handling one connection to completion
   before accepting the next, a *single* runner's long-poll stalls the admin UI and every other
   runner for the poll duration. P4 is therefore a correctness blocker for the runner protocol,
   not a throughput optimization. The runtime is well suited to the load — the M:N scheduler parks
   blocked sockets on the reactor, so many mostly-idle connections are cheap — only the accept
   loop needs to hand each connection to its own goroutine.
3. **Binary distribution needs every target built.** The server hands out governor, runner, and
   flow builds for each `(os, arch)` in the fleet, and Promise's `--target` cross-compilation is
   planned, not shipped. Until it lands, the release pipeline builds them **natively on a CI
   matrix** — which is exactly how Promise already produces its own linux-arm64 build, so this is a
   known-good path rather than a blocker. Cross-compilation, when it arrives, collapses the matrix
   to one job (P10). Gates are the deliberate exception: they are never distributed, because a gate
   is built from the commit it gates (see [base-engineering.md](base-engineering.md)).

Self-update also depends on P5's atomic replace — download to temp, verify the hash, rename over
the live binary, re-exec (`os.exec_replace` already exists) — and on P3's SHA-256 for verifying
what was downloaded. That the *governor* needs the hash check is worth noting: it is the least
updatable component in the system, so its integrity check is the one that most needs to be right
the first time.

## Gate execution — Reactor's half

The project declares its gates; **Reactor discovers, schedules, and executes them.** Reactor's
responsibilities end at the manifest boundary:

1. **Discover.** Run the project's gate-listing command in a registered worktree, validate the
   manifest, and create a `Gate` record per entry — merging any existing deployment-side overrides
   keyed by name. Re-run on new commits and on a slow refresh tick; new gates adopt with defaults,
   removed gates retire (history preserved), and changed metric semantics flag for admin review
   rather than silently invalidating baselines.
2. **Schedule.** Pick eligible gates per host OS × arch × deployment overrides, honoring each
   gate's declared cadence.
3. **Execute.** Run the declared preflight on a fresh worktree, then the gate command as a
   subprocess, parse the JSON output envelope from stdout, and write results to `LedgerStore`.
4. **Retain deployment-side config**, keyed by `(project, gate_name)` and layered *on top of* the
   manifest: arena assignment (the project says "I need linux/amd64"; Reactor decides *which*
   linux/amd64 arena), manual overrides (disable, narrow host match, force a cadence, adjust a
   ratchet cap, downgrade a metric during an incident, grant temporary exceptions), and metric
   history / baselines / ratchet state.

**Layering rule: the manifest defines the contract; deployment overrides constrain or annotate
it.** Overrides never *add* metrics or change a metric's direction — those are gate-contract
concerns owned by the project. Reactor never silently invents fields the project didn't declare.

The manifest schema, the output envelope schema, and the project-side gate SDK are specified in
[base-engineering.md](base-engineering.md).

## Platform requirements — requested of Promise

Reactor does not work around missing platform capability. Where Reactor needs something Promise
doesn't have yet, it is a **platform request**, listed here with the Reactor milestone it gates.
Reactor's design assumes these land; it does not design around their absence.

### Blocking

| # | Capability | Today | Needed for |
|---|---|---|---|
| P1 | **TLS — client and server** (PAL-mapped) | absent; `http` is explicitly "HTTP only (no TLS)" | **the whole fleet** — every runner and governor reaches the server over HTTPS — plus outbound GitHub and cloud-provider calls, and every inbound connection |
| P2 | **DNS resolution** | absent; `net.TcpStream.connect` requires an IPv4 literal | reaching any host by name |
| P3 | **`crypto` module** | absent; `std.Random` is documented as *not* cryptographically secure | webhook signature verification, session and API tokens, and self-update binary hash verification |
| P4 | **Concurrent HTTP server** | `http.Server.serve` handles connections **serially** | **long-polling is impossible without it** — one runner's poll would stall the whole server |
| P5 | **Atomic file replace + advisory locking + fsync** | `io` has no `rename`, no `flock`, no `sync` | the repo-backed stores' durability model is write-temp-then-rename; the lease ledger has concurrent writers |
| P6 | **HTTP client essentials** | no redirects, no keep-alive/pooling, no response gzip | GitHub API access at read-index volume, under rate limits |

**P1 — TLS**, mapped through the PAL to each platform's TLS stack rather than implemented in
Promise. Because the handshake, cipher suites, and certificate verification come from the OS, this
does **not** depend on P3 — the two are independent asks. The shape Reactor needs:

- **Client.** A stream type whose read/write surface matches `net.TcpStream` so it satisfies the
  same `Reader`/`Writer` structural interfaces — that is what lets `http` become generic over its
  transport and makes `https://` URLs work through the existing request path with no second client.
  Connect by hostname (P2) with SNI and hostname verification against the platform trust store.
- **Server.** A listener parallel to `net.TcpListener` that takes a certificate chain and private
  key and yields the same stream type on `accept`, so `http.Server` binds TLS by construction
  rather than by wrapping.
- **Errors.** Certificate and handshake failures surfaced as a distinct failable error, so Reactor
  can tell "this host is unreachable" from "this host's certificate did not verify" — they demand
  very different operator responses.

**P2 — DNS.** Resolution by name (`A`/`AAAA`), and `TcpStream.connect` accepting a hostname rather
than only a literal. TLS hostname verification depends on connecting by name, so P1 and P2 land
together in practice.

**P3 — crypto.** Independent of P1. Reactor needs SHA-256, HMAC-SHA-256, and constant-time
comparison — GitHub signs webhooks with `X-Hub-Signature-256`, and verifying that signature is the
*only* thing standing between the ingress endpoint and forged events — plus a CSPRNG for admin
session and API tokens, since `std.Random` documents itself as unsuitable for that use.

**P4 — concurrent server.** Today `serve` accepts a connection and handles it inline before
accepting the next. Reactor serves a UI, a machine API used by every running flow, and event
ingress from one process; serial handling makes any slow handler a full outage. The ask is a
per-connection goroutine with keep-alive support and a bounded concurrency limit.

**P5 — durable file operations.** `rename` (POSIX `rename(2)` / Windows `MoveFileEx` with
replace), advisory locking (`flock` / `LockFileEx`), and `fsync`. Every record write in the
repo-backed `ItemStore` and `LedgerStore` is write-temp → fsync → atomic rename; without it a
crash mid-write corrupts a record instead of leaving the previous version intact.

**P6 — HTTP client.** Redirect following, connection reuse, and gzip response decoding (the
`gzip` module exists and needs wiring into the client). GitHub's API is rate-limited and the
read-index makes many calls; one connection per request with no compression is not viable at that
volume.

### Non-blocking, wanted later

| # | Capability | Today | Needed for |
|---|---|---|---|
| P7 | KV / object-store client (`cloud`) | design only | the KV `ConfigStore`/`LedgerStore` backends |
| P8 | `toml` | stub | config ergonomics; JSON is sufficient meanwhile |
| P9 | `schema` | design only | manifest and API payload validation; hand-written validators meanwhile |
| P10 | `--target` cross-compilation | planned (runtime-architecture phase 7e) | collapses the runner/governor/flow release matrix to one build job; a native CI matrix works meanwhile. Matters more for flows, whose matrix multiplies per project |
| P11 | Tool-source-directory discovery + compile caching | see [promise-forge.md](promise-forge.md) | replacing the Go `./make` blueprint with `promise run <tool-dir>` |
| P12 | Addressing a module in a repo **subdirectory** | remote modules must have `promise.toml` at the repo root | consuming a Promise module from a Go-primary repo (flow, forge) without giving it a root manifest |
| P13 | Partial clone for remote modules | `git clone --bare`, full history, no `--filter` | any remote dependency pulls the entire repo and its history |

**P12 — subdirectory modules.** The containment semantics **already exist**: `CollectModuleSources`
skips any subdirectory carrying its own `promise.toml`, and the docs name these "nested modules".
What is missing is only *addressing* — there is no way to point a remote dependency at
`<repo>/<subdir>`. So this is a resolver change, not a module-system redesign. The design question
is where the subpath lives: in the location string (scales to N modules per repo, but needs an
explicit separator, since "where the repo path ends and the subdir begins" is not inferable for an
arbitrary git host) or as a field on the `[require]` entry (unambiguous, but one module per repo
because the key is the repo URL).

**Whether this is needed depends on repo layout.** Reactor and every flow share the wire types as a
module, and the flow common library is itself a module consumed per project — so module addressing
is on the critical path. A dedicated BASE repo with `promise.toml` at its root needs nothing new; a
module in a subdirectory of a repo that is not itself a Promise module does. The
[BASE-layer repo decision](base-engineering.md) therefore decides whether P12 is required at all.

### Already sufficient

`os` (process spawn with piped stdio, env, cwd, signals, exec, kill, wait), `io` (files,
directories, buffered readers/writers, metadata), `json`, `time` (wall clock, monotonic `Instant`,
`Duration`, sleep), `path`, `net` (TCP listener/stream with reactor-based goroutine parking),
`std` (`Mutex`, `Channel`, `Task`, `select`, `` `embed ``, `Builder`, collections). These cover
subprocess supervision, the repo-backed stores' data handling, and the concurrency model outright.

## Milestones

> **Not yet defined.** Sequencing waits until *what lives where, and what each piece is for* is
> settled — the [authority model](#authority-roles-steps-and-capabilities), the
> [topology](#deployment-topology--server-governor-runner), and the
> [BASE layer boundary](base-engineering.md) all move pieces across repos, and a build order drawn
> before those settle would only have to be redrawn.
>
> The platform requests above are independent of sequencing and stand as they are.

## Decisions locked

- **BASE is implemented in Promise end to end** — Reactor, runner, governor, and flows. A project's
  own gates may be in any language; that is the single deliberate polyglot boundary, and the gate
  contract (manifest + JSON envelope over a subprocess) exists to keep it open.
- **No FFI, no C bindings.** Everything is Promise-native or PAL-mapped to OS primitives. Missing
  capability is a platform request, not a Reactor workaround.
- **Seams stay process boundaries even though both sides are now one language** — the flow↔Reactor
  API because [authority](#authority-roles-steps-and-capabilities) needs an enforcement point, the
  gate boundary because gates must stay language-independent. The wire types are a module shared by
  Reactor and flows, not duplicated.
- **Two objectives govern**: a clean, reusable BASE implementation that applies to many projects,
  and **running reliably unattended for prolonged periods**. See [Objectives](#objectives).
- **Reactor is a new project, not a migration.** No compatibility with tracker is required — on
  disk, in APIs, or in data. Moving an existing hand-built process onto it is secondary.
- **GitHub issues = unified source of truth** (no sync world); the space can bifurcate later if
  ever needed.
- **PRs are first-class items** with their own identity; review artifacts are per-PR.
- **ItemStore = one identity authority per deployment** (GitHub *or* repo, never mixed) **+ an
  optional repo overlay** keyed by GitHub id for admin/private/large artifacts.
- **Reactor is cloud-only**, and one server serves every role; admin accounts and access control
  are required; tracker's OAuth plan is a useful starting reference.
- **Authority is role ∩ step**, declared and enforced outside the flow. *(Proposed —
  see [Authority](#authority-roles-steps-and-capabilities).)*
- **A Reactor server is always in the picture** — there is no serverless variant. *(Proposed —
  see [No serverless variant](#no-serverless-variant).)*
- **Gates measure the tree, so they come from the tree; flows modify the tree, so they come from
  outside it.** Gates are built from the commit under test and never distributed prebuilt. Flows
  are project-specific but versioned outside the project source, so a flow fix never contends with
  in-flight work in a worktree.
- **Reactor distributes flow binaries**, keyed by `(project, flow, os, arch)`, over the same
  channel as the governor and runner builds. The runner resolves the flow **per step, not per
  item**, so a mid-resolution fix is picked up by an item's remaining steps.
- **Reactor is three deployables, not one** — `bin/reactor` (cloud), `bin/runner` and
  `bin/governor` (in the workspace, on other machines or containers).
- **The server never reaches into a host.** Runners always initiate outbound and long-poll. Cloud
  arena provisioning is the sole exception, since there is no host to run an arena-host runner on.
- **Keep cloud arenas** — mostly implemented, and the practical way to run cross-platform gates.
- **All four repos (promise/flow/forge/reactor) are public**; cross-repo deps are versioned
  dependencies, not submodules.
- Build tooling is the **forge blueprint** (`./make`, `bin/verify`, ratcheted baselines, guard).
