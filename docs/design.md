# Reactor — Architecture

> **This document defines what Reactor is** — the implementation language, the process seams, the
> authority model, the deployment topology, the persistence split, the reliability rules, and how
> steps and gates are executed. Reactor is written in **Promise**.
>
> **It assumes** the BASE layer: flows, the gate manifest contract, and what a project adopting the
> methodology provides — all in [base-engineering.md](base-engineering.md).
> **Depending on it:** [engagement-feed.md](engagement-feed.md), which specifies the human-facing
> half in detail. Dev tooling is [promise-forge.md](promise-forge.md).
>
> What is undecided is in [Open questions](#open-questions); everything else here is a statement
> about the system. Progress lives in the [README](../README.md#status), not here.

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
mid-resolution. What that objective demands mechanically —
[never stall, never spin](#reliability--never-stall-never-spin) — is its own section.

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
  copies. That module's home is the [BASE layer repo](base-engineering.md), addressed as a
  subdirectory module now that [P12](#platform-requirements--requested-of-promise) has landed.
- **The GitHub client is written once, in Reactor.** With
  [no serverless variant](#no-serverless-variant), flows never talk to GitHub directly — they go
  through the Reactor API, and Reactor owns the only GitHub client.
- **Nothing is constrained by an existing on-disk format.** Persistence shapes are chosen for
  clarity and for the conformance suite. There is no predecessor layout to match.

The no-FFI fact still binds wherever another language legitimately remains — a project's own gates,
and whatever Go tooling survives.

### A shared module is not a shared version

One shared module removes hand-synchronization; it does **not** remove version skew, and conflating
the two would be the expensive mistake here. Promise rejects conflicting pins *within one
compilation*, but Reactor and a flow are separate compilations producing separate binaries — so a
flow built against one commit of the wire module talking to a Reactor built against another is
invisible to the module system. Nothing errors at build time; the mismatch arrives as a malformed
field at runtime.

And skew is not a hazard to guard against, it is **guaranteed by three decisions already made**: the
flow version resolves [per step](base-engineering.md#the-principle), so one item spans versions; the
runner self-updates *after* a server upgrade, so runner and server routinely differ; and companion
repos build on their own cadence, so Reactor cannot force a rebuild.

> **The wire carries a `schema_version`, Reactor refuses unknown majors, and evolution within a
> major is additive-only** — never remove a field, never repurpose one.

That is deliberately the same rule the [gate
manifest](base-engineering.md#gate-discovery--the-project-declares-reactor-discovers) already
follows. The asymmetry — a versioned contract for gates and an unversioned one for flows — was an
oversight, not a decision. Additive-only is what makes a one-version skew safe by construction
rather than by luck.

**Persisted step state inherits the same requirement.** If step 3 runs under one flow version and
step 4 under the next, then what the first wrote must be readable by the second. Per-step resolution
is what makes an async flow fix useful, and state compatibility is what makes that survivable rather
than merely acknowledged.

## Authority: roles, steps, and capabilities

> Reactor resolves work items of any kind, and *who may do what* is configuration rather than a
> built-in scenario. One concrete arrangement — how these primitives get configured for a public OSS
> project — is worked through in [base-engineering.md](base-engineering.md) as an example.

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
the work. A step is one way work arrives; [a human acting
directly](#a-human-acting-directly-is-bounded-the-same-way) is the other, and it intersects the same
way.

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

### A human acting directly is bounded the same way

A step is not the only thing that mutates an item. A person answers a question, takes an action from
a [feed card](engagement-feed.md#authority-over-article-actions), or clicks something in the admin
UI — and none of that runs inside a step, so `role ∩ step` has nothing to intersect. Read literally,
the model would either not cover them or leave them bounded by role alone.

The generalization is small, because the second factor was never really *a step*. It is **the work's
own grant**, and a step is one form work takes:

> **Effective authority = role ∩ grant.** For agent work the grant is the **step**; for human work
> it is the **action**, which declares what it requires exactly as a step does.

Bounding a person by role alone would be simpler and would give up the property that makes the model
worth having. An admin running `plan` cannot edit source because *planning is not editing* — and by
the same reasoning an admin clicking *File bug* on a card is filing a bug, not exercising every
capability their role permits. The card is a shortcut to one operation; the operation states what it
needs.

Two consequences:

- **An action's requirement is declared where a step's grant is** — outside the reach of whatever
  proposed it. An article is posted by an agent, so an article carrying its own grant would be the
  constrained party writing its own permission slip. **An article names an operation, never a
  capability**, and Reactor resolves what that operation requires from its own config.
- **The static check extends.** Reactor already [refuses to dispatch a flow to a role that cannot
  complete it](#where-it-is-enforced); the same check decides which actions a reader may be offered,
  so a card renders only what its reader could actually invoke.

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
| Reactor API | **Per-call validation** against role ∩ step, or role ∩ action for a human-initiated call | every item mutation |
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

### The capability vocabulary

The model is only as good as the resource/verb vocabulary it is expressed in. The four questions
this table left open are settled below; the vocabulary itself will still grow as new resources
appear, and each addition has to answer the same question — what does this let an agent reach that
`role ∩ step` could not otherwise describe?

| Resource | Capabilities |
|---|---|
| Item | read · create · annotate:`<kind>` (plan, inspection, review, note, **question**, **answer**) · state (open/close/reassign) · artifact write:own · checkpoint write:own · **routing** (`flow:` labels, assignee) |
| Source tree | read · write:`<glob>` (allow) minus `<glob>` (deny) |
| VCS | commit · `branch.create` · push:branch:own (may be non-fast-forward) · push:origin (never) · pr.create · pr.merge |
| Gates | run · results.read · baseline.write · exception.grant |
| Orchestration | item.claim · step.dispatch · arena.provision |
| Deployment | config.read · config.write · secret:`<name>` · `budget.extend:<limit>` · host.adopt |
| Engagement | article.post · article.resolve:own · `article.act:<operation>` |
| Tool surface | `mcp:<server>/<tool>` · shell · `net.egress:<host>` · `fs:<path>`:read/write |

**`annotate:answer` is authority, not annotation.** An answer steers autonomous work, so who may
write one is gated exactly like any other grant — and on a public code host, where anyone may
comment, it is the only thing separating a maintainer's decision from a stranger's. The
[engagement feed](engagement-feed.md#authority-over-article-actions) carries the rest: a question
declares the role that may answer it, and effective authority is that intersected with the
answerer's own.

**The tool-surface row is not optional decoration.** Everything an agent reaches through its harness
is reach that `role ∩ step` cannot describe if it is unnamed, and an MCP server is the clearest
case: it can grant filesystem write, network egress, or database access without touching a single
other row in this table. The choke point already exists — [tool
availability](#where-it-is-enforced), never expose the tool at all — and it is the strong kind, in
the same class as credential scoping, because you cannot call a tool that is not in your list. What
was missing is the declaration, not the mechanism.

Naming it gives the static capability check below an operational form: at dispatch, Reactor
verifies *mounted tool set ⊆ step grant* and refuses to launch the session otherwise. A `plan` step
that would come up with a filesystem-writing server attached fails before the agent starts rather
than after it writes something.

**MCP breaks an assumption every other row makes.** The rest of this vocabulary is closed —
Reactor defines it. MCP tools are declared by the server at runtime, and that difference is
load-bearing:

- **Grants are allowlists against a pinned server, never denylists.** With a floating server, an
  upstream release that adds `execute_sql` silently widens every step that mounts it — a
  supply-chain-shaped widening no diff in either repo would show. Pin version and hash; an unknown
  tool is not exposed. Fails closed.
- **Read versus write cannot be inferred.** `search_files` reads and `create_file` writes, but a
  single `run_query` does either depending on its argument, and the tool schema will not say. The
  classification is **declared by whoever mounts the server**, not derived from the server's own
  metadata, and anything unclassified is write.

Where the servers come from, and why a tree-provided one may only ever be granted read, is in
[base-engineering.md](base-engineering.md#bounds-are-authority-not-tooling).

Four questions the vocabulary left open, now settled.

**Tree write is path-scoped, as a broad allow-list minus deny carve-outs.** A step names globs it
may write — `**` and `*.pr` are both legitimate answers, so breadth costs one character — and a
deny list carves out what stays untouchable inside them. **Deny always beats allow**, and **absence
of an allow entry means no tree write at all**, so `plan` gets its "cannot touch the tree" for free
and a step that writes says so even when it says `**`. The point of the carve-outs is to convert
in-tree controls from detective to preventive: a gate baseline, a CI workflow, or the
[`.base/` pointer](#a-repo-is-not-a-project-until-it-is-adopted) edited silently is caught by review
today, and refused outright once it is denied. Guard enforces at write time for
fast feedback, but **the authoritative check is the resulting diff at the step boundary**, because a
path check alone is escapable by rename.

**Routing fields are their own capability, because they are not item data.** Eligibility is decided
by `flow:` labels plus assignee, so a step that can write them can relabel its own item into a flow
its role may not run — a silent, immediate widening achieved without touching any authority config.
Everything else stays kind-grained; a vocabulary that tracks the item schema field by field would
churn and get skimmed.

**Roles are flat, explicit sets — they do not inherit.** A ladder would be more concise, but
inheritance is a mechanism for widening capabilities silently: adding one to a base role grants it
to every role above, and the diff that did it touches a role nobody was reviewing. That is the
failure this model treats as categorically worse than any other, and there are few enough roles that
duplication is a lint rather than a cost.

**A conditionally-needed capability is declared at its worst case, and the step splits when that
matters.** Runtime escalation would be tighter, but it destroys the static check below — a step
whose needs appear at runtime cannot be validated against a role in advance — and it puts the
constrained party in charge of initiating its own widening. Where over-granting genuinely costs
something, the project splits the step into a narrow common path and a wider exceptional one, which
this design already permits and which needs no new mechanism.

**Where the declarations live is part of the model, not a packaging detail.** Role grants and step
grants must sit somewhere the agents they constrain cannot reach — otherwise an `implement` step
could widen its own grant, and the bound would be self-authorizing. Since flows operate on the
*project* worktree, the declarations belong outside it: see
[What lives where](base-engineering.md#what-lives-where).

### What a flow declares, and what is declared about it

A step carries two kinds of declaration and they cannot share a source, because one of them is the
flow describing itself and the other is the system constraining the flow.

> **A flow is never trusted to limit itself.** So anything that *bounds* a step is read
> independently of the flow, and anything that merely *describes* it comes from the flow.

| | Declared by | Contents |
|---|---|---|
| **Operational** | the flow binary, via a `describe` command | item types, eligibility, `serialized_by`, fresh-session and arena-independent hints, and how each step's artifact is [verified](base-engineering.md#6-a-steps-completion-is-a-verified-artifact) |
| **Authority** | companion-repo config Reactor reads on its own | step grants, per-role capabilities, read scope |

**The role *vocabulary* is deployment-owned; the grants attached to each role are project-owned.**
A companion repo declares what `reviewer` may do *here*; it does not mint the name. A principal
holds an account on the Reactor server and therefore a role per project, so a vocabulary defined
per project would give "who is this person" as many answers as there are projects — and any
deployment-wide surface, the [engagement feed](engagement-feed.md#audience-and-tags) above all,
would have no single way to say *for me*. The names live in
[ConfigStore](#configstore--the-deployment-owners-residual) alongside admin access control; the
grants stay in the companion repo, which keeps both out of reach of the agents they bind.

`role ∩ step` therefore means **the role in the item's project**, and a fleet-scope role covers
conditions that belong to no project — an arena declared lost, quota exhausted, a governor
crash-looping.

The declared/constrained split is not fussiness either. A flow emitting its own grants would be the
constrained party describing its constraints, and Reactor could then check a step only against what
the flow chose to admit.
Conversely, operational facts are best known by the code implementing them — keeping them in the
binary is what stops a step's declared exclusions from drifting from what the step actually does.

`describe` is deliberately symmetric with `bin/gate list --json`: a subprocess that emits a manifest
Reactor validates. That symmetry is worth preserving, because it means one discovery mechanism
serves both halves of the system.

## The states, and what they belong to

States are introduced throughout this corpus where they are needed, and written in one register — so
`blocked`, `contended`, `defaulted`, and `offline` read as one vocabulary. They are not. **They
belong to six different lifetimes**, and confusing them is the hazard: a scheduler that treats a
queued step like a blocked item, or an article's disappearance like a question's answer, is wrong in
a way no individual rule catches.

This is the enumeration. Transitions are governed by the rules in their own sections; what follows
is which states exist and what they are *about*.

**Item** — the durable work unit. Public state is the code host's (`open`/`closed`); everything else
is the [private overlay](#itemstore--composite-identity-github--private-overlay) keyed by it.

| State | Means | Dispatchable |
|---|---|---|
| `open`, unclaimed | exists, nobody holds it | **yes**, if labels and assignee make it eligible |
| `open`, claimed | leased to an arena, in resolution | yes — to its [bound arena](#an-arena-is-leased-to-an-item-not-to-a-step) |
| `open`, **paused** | *derived* — carries one or more **holds** | no |
| `closed`, **resolved** | every declared step satisfied | — |
| `closed`, **declined** | closed without doing the work | — |
| `closed`, **moved** | [relocated](#relocation-is-a-link-not-a-closure), carrying a pointer to its successor | — |

### Paused is derived; the holds are what exist

An item is not blocked *or* waiting *or* parked. It can be all of them at once — queued behind
another project, holding an unanswered question, and carrying a fault somebody has to look at. So
the reasons are not an enumeration to pick from, and the pause is not a thing anyone writes down:

> **A hold is a named reason an item cannot be dispatched, carrying the condition that clears it.
> `paused` is *derived* from holding at least one, and an item resumes when the last one clears.**

**Nothing ever sets `paused`.** A stored pause flag is a second copy of a fact the holds already
carry, and the two can disagree — the same failure as [a lock that is a flag rather than a
lease](#every-exclusion-is-held-by-a-process-never-by-a-flag), where nothing releases what nobody
holds. Here the holds *are* the truth and the state is a question you ask them.

It is also the mirror of how a step runs: a step acquires a *set* of
[exclusions](#exclusions-are-declared-and-waiting-for-one-is-not-work) and proceeds when it holds
them all; an item carries a *set* of holds and proceeds when it holds none.

**Four kinds, because they are four different diagnoses:**

| Kind | Says about the work | Cleared by | A fleet with forty |
|---|---|---|---|
| `blocked` | nothing is wrong; it is queued behind something | the dependency [landing or publishing](#an-edge-names-a-target-and-a-condition-never-a-version) | may be perfectly healthy — deep dependency chains look like this |
| `waiting` | nothing is wrong; a person is an input | an answer arriving, or [the window defaulting](engagement-feed.md#questions-with-deadlines) | means the human is the bottleneck |
| `parked` | **something is wrong** | a human diagnosing it | is a sick fleet |
| `manual` | a person took it over — *"I'll handle this"* | that person releasing it | is a fleet doing by hand what it cannot yet do itself |

**`manual` is not an assignee, and it is not a fault.** An assignee is
[routing](#the-capability-vocabulary); a `manual` hold stops dispatch outright. Nothing has gone
wrong and nothing is missing — someone has simply decided to do this one themselves, which is a
legitimate and permanent part of the system rather than an escape hatch. Like `parked`, no
mechanical condition clears it, and that is the point: a person took it, a person returns it.

It is also **the honest measure of what the system cannot yet do autonomously.** For a project whose
thesis is that agents build and maintain large software, the count of work a human took back by hand
is the least flattering and most useful number available — worth tracking deliberately rather than
discovering later.

A `manual` hold **releases the arena binding** at the next clean boundary, by the same reasoning as
[blocking](#blocked-is-a-recorded-state-not-a-stall): the agent's accumulated session is worth
nothing to the person now doing the work, so holding capacity for it is pure waste.

Reporting only *forty items paused* is the number that tells an operator nothing. Counting holds by
kind is the diagnosis — and the counts legitimately **sum to more than the item count**, which is
information rather than an error: an item carrying three holds is in worse shape than one carrying
one.

- **Every hold names what clears it**, and a hold whose condition can never be evaluated is refused
  at creation — the same rule that [refuses an edge closing a
  cycle](#blocked-is-a-recorded-state-not-a-stall), and what keeps a pause from being a
  [stall](#reliability--never-stall-never-spin). `parked` is the deliberate exception: its condition
  is a human, which is exactly what it is for.
- **Clearing one hold is progress and is recorded as such**, even though nothing dispatches. An item
  that went from three holds to one is moving; an item that has held the same three for a week is
  not, and only per-hold history can tell them apart.
- **Each hold that needs a person is its own [article](engagement-feed.md).** One item can raise two
  — a question for its author and a fault for an operator — because they need different people and
  different actions. A single per-item notification would force them into one card that neither
  reader can act on.

**Holds exist at project scope too.** A [pairing
disagreement](#a-repo-is-not-a-project-until-it-is-adopted) or a withdrawn adoption holds the whole
project, and nothing in it dispatches for the duration — recorded once, on the project, rather than
copied onto every item it stops.

`stuck` is not a fourth kind: [loop detection](#every-attempt-must-make-progress) tripping is one of
the ways something goes bad, so it is a **reason for parking** alongside an unclassified failure or
a refused grant. And **a grant extension is a question**, not a fault, so it takes a `waiting` hold
— which means the [grant ladder](#the-grant-ladder) rides the same mechanism as any other decision
routed to a human: a required role, a window, and a recorded answer.

**The test for whether a kind belongs is one the design already uses.**
Plenty of other things stop work — a provider outage, an exhausted quota, an unreachable bound
arena, a step losing a race for a contended lock — and **none of them place a hold**, because none
say anything about *the work*. A hold earns its place by answering *what is true of this item*;
anything that answers *what is true of the fleet* belongs elsewhere. That is the
[infrastructure-versus-process test](#infrastructure-failures-and-process-failures-are-different-things)
applied to state rather than to failure: an item whose arena went away stays `claimed`, and it is
the arena that is unhealthy.

**Three terminal states, and the distinction is load-bearing.** An item closed for being in the
wrong repo must not read later as one that was refused, which is why `moved` exists alongside
`declined` rather than collapsing into it.

**Step run** — one dispatch of one step. Two vocabularies here, and they answer different questions:

| | Vocabulary | Answers |
|---|---|---|
| **Protocol** | `satisfied` · `unsatisfied` · `advanced` · `blocked` — plus the scan's `complete` and `handoff` | what the step *reported* ([step resolution](base-engineering.md#step-resolution--steps-dispatch-themselves)) |
| **Process verdict** | clean exit · non-zero exit · deadline kill · signal · disappeared-without-status | how the *process* ended ([nothing runs unwatched](#nothing-runs-unwatched)) |

`contended` belongs to neither: it is a step that exceeded its **queue** deadline without starting,
returned to the queue as a [capacity signal rather than a
defect](#exclusions-are-declared-and-waiting-for-one-is-not-work). It says nothing about the item.

**Question** — `open` · `answered` (a principal answered) · `defaulted` (the window elapsed and the
system answered) · `withdrawn` (the asker retracted). `answered` and `defaulted` produce an
identical selection and must never merge, because the second is what
[calibration](engagement-feed.md#the-feedback-loop-calibrates-estimates-never-the-objective) learns
from.

**Article** — present or absent, and nothing else. Rank and position are computed per read, so
*below the fold* is not a state. See [the feed's store](engagement-feed.md#store).

**Arena** — `leased` · `reserved` · `idle` · `offline`, holding at most one lease.
See [the four states](#an-arena-is-in-exactly-one-of-four-states).

**Host** — adopted or not, which is a trust decision that outlives everything above it. Registration
and retention are separate clocks over the same host; see
[adoption](#a-host-is-not-an-arena-until-it-is-adopted).

**Eligibility, stated once:** an item is dispatchable when it is `open`, holds no holds, is either
unclaimed or claimed to the arena being offered, and its `flow:` labels and assignee match what the
flow declares. Everything else in this section is context for one of those clauses.

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
owner's** choice — quota and cost limits, [model accounts](#model-accounts--subscription-and-api)
and their credentials, [which hosts are adopted](#a-host-is-not-an-arena-until-it-is-adopted),
arena allocation and provider creds (including [how long an arena record is retained while its host
is absent](#a-host-that-is-merely-off-is-not-a-host-that-is-gone)), admin access control. Flows and
gates are **not** here; they live in the project.

### LedgerStore — per-server active state

Lease ledger, gate run history and baselines, orchestration/scheduler run state, turn registry,
quota snapshot, the [engagement feed's](engagement-feed.md#store) article store, the GitHub
read-index cache. CRUD-shaped and hot. Implementations:
repo-backed (`_*.json`) and a KV example.

## No serverless variant

> A Reactor server is always in the picture. There is no mode in which a contributor clones a
> project, runs a flow against GitHub directly, and never touches a server.

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
                             │  HTTPS, always initiated from the workspace (long-poll)
        ┌────────────────────┼────────────────────┐
        │                    │                    │
   ┌────┴─────┐         ┌────┴─────┐         ┌────┴─────┐
   │ governor │         │  runner  │         │ governor │   supervises runners · swaps binaries
   │  runner  │         └──────────┘         │  runner  │   executes work in a worktree
   │  runner  │                              └──────────┘
   └──────────┘

    bare metal            container            cloud VM
   (a host: one         (an arena, not       (a host Reactor
    governor, two        a host — it has      provisioned, so
    arenas)              no governor; an      adopted by
                         arena-host runner    construction)
                         creates and
                         destroys it)
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
  - **Flow binaries** — keyed by `(project, os, arch)`. **One binary per project**: a flow is
    self-describing, so a single binary can carry the resolution for every item type and every
    step the project defines. The runner identifies which project it is working on from the
    worktree's origin (and refuses to guess when it cannot), then fetches that project's current
    binary for its platform. The version is resolved **per step rather than per item**, so a flow
    fixed mid-resolution is picked up by the remaining steps of work already in flight — see
    [base-engineering.md](base-engineering.md#the-principle) for why that matters.

  Reactor **builds neither class**. Each is built by its own repo's CI and published as a release;
  Reactor ingests the release, verifies it, and serves it onward. See
  [Who builds flows](base-engineering.md#who-builds-flows).
- **Ingress for GitHub events** — PR-open and related signals routed to the scheduler.

**Runner** (`bin/runner`, one per workspace). Long-lived. Registers with the server advertising its
host OS, arch, role, and capabilities — and is served nothing until its host is
[adopted](#a-host-is-not-an-arena-until-it-is-adopted) — then long-polls for actions: run a flow
binary, run a gate, prepare a worktree, provision an arena (in the arena-host role). Streams output
back as it goes.
Every one of those actions is a **child process the runner spawns, watches, and bounds by a
deadline** — the runner does no work in its own address space, and holds no wait it cannot tie to a
live pid ([Reliability](#nothing-runs-unwatched)).
**The runner self-updates** — that is what makes shipping new runner code to a deployed fleet
automatic after a server upgrade, with no operator work per host.

**Governor** (`bin/governor`, **one per host**). A minimal supervisor: fetch each runner binary on
first start, keep it alive, and swap it when an update is staged (a distinguished runner exit code
means "update staged — swap and restart"; a crash restarts with backoff, and auto-rolls-back if
the crash follows an update). A governor serves every runner in its scope, and there are two
arrangements: **system-wide**, launching each runner as its own configured OS user through the
platform's service manager, or **per user**, where each OS user runs their own governor and needs
no privilege whatever. The privileged form is still a static arrangement with no per-spawn decision
and no credential to attach, which is why the objection that stops [a runner from spawning steps as
other users](#the-account-belongs-to-the-arena-and-that-is-forced) does not carry up to this layer.
Either way it knows nothing about items, gates, or arenas. It is operator-installed and does
**not** self-update, which is precisely why it must stay small and change almost never.

**A governor is how a machine appears to the fleet** — the thing that announces it, bears its
[adoption](#a-host-is-not-an-arena-until-it-is-adopted), and supervises what runs on it, while each
runner announces its own arena underneath. A governor alive with no runner alive is then a state
worth being able to see: it distinguishes "the machine is off" from "the machine is up and its
runners keep dying", which is exactly the signal the
[write-off accounting](#a-host-that-is-merely-off-is-not-a-host-that-is-gone) wants and cannot infer
from silence.

**But a governor is not the same thing as the machine, and conflating them is a bug with teeth.**
Two governors can share a box — deliberately, one per OS user, which is the arrangement that needs
no privilege; or by accident, someone starting a second while the first is running. Neither may end
up supervising the same runners twice, and neither may be mistaken for two independent machines.

> **A governor holds an exclusive lease over the runners it supervises, keyed `(machine, os user)`,
> one per user in its scope. `host:` exclusions are keyed by the machine and never by the
> governor.**

- **Overlap is refused, not merged.** A per-user governor holds one lease; a system-wide governor
  holds one per user it manages. A second governor over any of the same users fails to start rather
  than quietly doubling the fleet's idea of local capacity — which is the shape of failure that
  shows up later as inexplicable contention rather than as an error.
- **The local half of the lock is taken before the server is reached.** The governor is what
  recovers a machine, so a double start must fail with the network down too. A host-local exclusive
  file lock does that; the server-side lease is the second choke point, in the same spirit as
  [enforcement being plural](#where-it-is-enforced) rather than resting on one mechanism.
- **The server-side half catches what a local lock cannot: two machines presenting one identity.**
  A cloned VM image or a restored snapshot carries its adoption with it, so without this the way to
  mint adopted hosts is to copy one. The second claimant is refused and must be
  [adopted](#a-host-is-not-an-arena-until-it-is-adopted) as what it actually is — a new machine —
  and the refusal is recorded rather than silently retried.
- **`host:cpu` and the per-host verify lock arbitrate *physical* contention**, so they key off the
  machine. Two governors on one box share them; splitting supervision by user must not double the
  box's apparent capacity. **A sandbox inherits its parent machine's `host:` identity** for exactly
  the same reason — eight containers on one machine are eight arenas contending for one CPU, not
  eight independent hosts, and keying those exclusions to the sandbox would oversubscribe the
  machine by a factor of eight while every lock looked correctly held.

**Sandboxes are managed by a runner, never by the governor** — the governor knows nothing about
arenas, and provisioning one is arena knowledge. So supervision splits by layer, and the split is
the useful one:

| | Supervises | Repair available |
|---|---|---|
| **Governor** | the runner processes on its host | restart, swap the binary, roll back a bad update |
| **Arena-host runner** | the sandboxes on its host, and the runners inside them | everything the governor can do, plus create and **destroy** — the repair no process inside a sandbox can perform on itself |

> **A sandbox never carries a governor. Its arena-host runner is its supervisor**, and is strictly
> the more capable one.

- **The rule is not about lifecycle.** A sandbox may be ephemeral or may stand for months, and a
  deployment may reasonably run *every* arena in one, as an isolation posture rather than a
  lifecycle choice. Deciding governor placement by how long a sandbox lives would put a governor
  inside each of a dozen standing containers on one machine — an operator-installed copy, baked
  into an image, of the one component that
  [cannot self-update](#deployment-topology--server-governor-runner). That is the worst place in the
  system to multiply.
- **What decides it is whether anything outside is positioned to supervise.** A local sandbox's
  arena-host runner is right there on the box: it can stop the runner inside, swap its binary,
  restart it, and — uniquely — destroy and recreate the whole sandbox. A cloud VM has nothing local
  above it and the server may not reach in
  ([the outbound-only invariant](#deployment-topology--server-governor-runner)), so it must carry
  its own. **Governor placement falls out of that invariant rather than being a separate policy.**
- **Recreate-versus-update stays a real question, just a different one.** It is the arena-host
  runner's choice of repair, not a question about topology: **is recreating this sandbox cheaper
  than updating it in place?** A container serving one resolution is cattle and gets recreated; a
  standing one holding warm caches and materialized worktrees is updated where it stands.
- **Supervision from outside is the only kind that survives the sandbox wedging**, which is the same
  reason [a hung agent must be terminable by something that is not itself
  hung](#seams-are-process-boundaries--by-design-not-by-accident). An in-sandbox governor would be
  inside the wedge it is meant to repair.

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

### A host is not an arena until it is adopted

A governor coming up on a new machine can do exactly one thing: announce itself. Until the
deployment owner **adopts** that host it holds nothing, runs nothing, and is served no binaries. An
unknown machine asking for source, credentials, and work is a trust decision, not a registration
detail, and answering it automatically would hand fleet capability to anything that can reach the
server. Three terms sit on top of one another here, kept distinct because their lifetimes differ by
orders of magnitude:

| | What it is | Lifetime |
|---|---|---|
| **Adoption** | the deployment owner admitting a host to the fleet | once; survives everything below |
| **Registration** | a runner announcing itself on start — os, arch, role, capabilities — and being issued or rejoining an arena record | every runner start |
| **Retention** | how long an arena record outlives its runner's absence before the arena is [declared lost](#a-host-that-is-merely-off-is-not-a-host-that-is-gone) | hours; **default 24** |

- **Reactor's own ephemeral arenas are adopted by construction.** The server provisioned the VM, so
  it already knows the identity it is about to meet: the arena presents the credential minted for it
  at provisioning and is admitted with no human in the loop. That is the *only* automatic path, and
  it is safe precisely because the adopting party created the thing it adopts.
- **Declaring an arena lost does not un-adopt its host.** The arena record dies; the trust decision
  does not. A laptop shut for a long weekend comes back, registers, and is issued a **new** arena,
  with no operator involved and
  [nothing resumed](#a-host-that-is-merely-off-is-not-a-host-that-is-gone). Conflating the two would
  turn every write-off into an administrative task and let the fleet degrade by attrition every time
  somebody took leave.
- **Withdrawing adoption is the deliberate act, and how a bad machine leaves the pool.** A host that
  accumulates write-offs or orphaned processes is a machine to take out of service. That is a
  revocation of adoption rather than an arena-level state, which is what makes "take it out of the
  pool" something an operator can actually do rather than a thing they keep re-discovering.
- **The adoption record names the host's default arena state**, and that one field is what keeps a
  closed default from turning into per-arena paperwork. A build box says `idle`, so every workspace
  that comes up on it joins the pool; a laptop says `reserved`, so every workspace that comes up on
  it belongs to the person until they hand it over. **Adopting a machine is a statement about trust,
  not an enlistment of everything running on it** — collapse the two and adopting a laptop starts
  dispatching work into the workspace its owner is sitting in.
- **Adoption records are the deployment owner's**, held in
  [ConfigStore](#configstore--the-deployment-owners-residual) with arena allocation and provider
  credentials — not in the project, and not inferable from anything a host says about itself.

### A repo is not a project until it is adopted

The same rule as above, one level up, and for the same reason: what enters a deployment is a trust
decision rather than a registration detail. Someone with the grant points Reactor at a repo once;
from then on the repo speaks for itself, by carrying a
[`.base/` config](base-engineering.md#the-base-directory--how-a-project-names-its-setup) naming the
companion repo that holds its authority.

> **Until an admin adopts the pairing it is a request, not a project.** Its issues are not work,
> nothing dispatches, and the request surfaces as an article addressed to the role that may adopt.

- **The claim is in the tree; the fact is not.** `.base/` lives where agents write, so it can never
  be the record. Reactor holds the pairing in
  [ConfigStore](#configstore--the-deployment-owners-residual) beside host adoption, and honors the
  in-tree pointer only where the two agree.
- **`.base/` is a denied path in every tree-write grant.** No step may edit it, and as with every
  other carve-out the authoritative check is [the diff at the step
  boundary](#the-capability-vocabulary) rather than the path, since a path check alone is escapable
  by rename.
- **A disagreement pauses the project.** If the tree names a companion the deployment record does
  not, nothing in that project dispatches until an admin resolves it. Failing closed is right for
  the same reason [an unclassified failure is treated as a process
  failure](#infrastructure-failures-and-process-failures-are-different-things): you do not know
  which side is wrong, and continuing means running agents under grants that may not be the ones
  intended.
- **So `.base/` is a tripwire, not authority** — which is what makes it worth having despite being
  untrusted. If it could never disagree it would be decoration; because it can, an attempt to
  repoint a project at a more permissive companion is loud and immediate instead of silent.
- **Withdrawing adoption is how a project leaves**, and it stops dispatch the same way. Recorded
  like any other reclamation.

### Adopting a project admits its people

Adoption does a third job, and leaving it implicit would mean maintaining a user list — a copy of
something the project already maintains, drifting the moment anyone's access changes. That is
exactly the [mirrored-project-knowledge](base-engineering.md#no-manual-gate-registration) failure
that no manual gate registration exists to prevent, so it gets the same answer.

> **Authentication is the code host's. Role *assignment* is derived from repository permission
> through a deployment-owned mapping. The vocabulary stays the deployment's; the assignment is the
> project's.**

Adopting a project therefore admits its collaborators, and there is no separate act granting someone
access to the admin UI: whoever resolves to a role in an adopted project can sign in, sees what
[read scope](base-engineering.md#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped)
allows, and may do what their role permits. Access is not a fourth thing to administer — it falls
out of the other three.

- **A mapping, never an identity.** `write → contributor`, `maintain → reviewer`, and so on. Host
  permissions are coarse and are not this system's vocabulary, so they supply *who*, and the
  deployment supplies *what that means* — the same division as a project declaring gates and Reactor
  scheduling them.
- **An unmapped permission yields no role, and no role means no access.** This matters most where it
  is least obvious: on a public repository, read permission is *everyone on the internet*. Public
  read is not an observer role unless a deployment deliberately maps it. Fails closed, like
  [every other authority identifier](engagement-feed.md#a-degraded-path-is-never-a-silent-path).
- **Derived, never stored as a second copy.** A role is resolved when it is checked; the
  [read-index cache](#ledgerstore--per-server-active-state) may hold it, and that cache is
  **timed** — never authoritative — so revocation propagates on expiry rather than requiring anyone
  to remember to mirror it. A stored role assignment is the same defect as a stored `paused` flag:
  a second copy of a fact that can disagree with the fact.
- **The escalation floor is assigned, never derived.** The rule that
  [one role must always exist with a live principal behind it](engagement-feed.md#four-rules-that-close-the-remaining-gaps)
  cannot be satisfied by derivation: a code-host outage or one permission change would remove the
  last admin, and the deployment would lose the ability to fix its own authority config. At least
  one principal is assigned deployment-side and depends on nothing external.
- **Derivation is one source, not the mechanism.** A [GitHub-free
  deployment](#itemstore--composite-identity-github--private-overlay) assigns directly, and the rest
  of the model does not change — which is the test that the mapping is a convenience rather than a
  load-bearing assumption.

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

## Model accounts — subscription and API

> Subscription first; API is a second `kind` rather than a second design.

Agent work is bought two ways, and they are not the same resource. A **subscription** is a flat fee
for a token budget per rolling window — much cheaper per token, capped, and **perishable**: quota
not used inside a window is gone. An **API account** bills per token against a spend cap — elastic,
with no window to waste, and materially more expensive. A fleet wants both, and wants several
subscriptions running at once.

```
ModelAccount
  id          "sub-a" | "api-prod"
  kind        subscription | api
  provider
  quota       subscription: rolling <period>, <token budget>
              api:          <currency> per <period> spend cap
  cost_model  flat-fee-per-window | per-token
```

Accounts live in [ConfigStore](#configstore--the-deployment-owners-residual) with the rest of the
deployment owner's residual. **Reactor holds the reference, never the credential** — the material is
provisioned into the arena and Reactor ships no token, so credential scoping stays the strongest
[choke point](#where-it-is-enforced) and a compromised server leaks no accounts.

### The account belongs to the arena, and that is forced

Agent harnesses authenticate at the level of an OS account — credentials in a home directory or a
keychain — so an account cannot be chosen per request. It is a property of the environment the step
runs in, which is precisely the premise the authority model already rests on: **capability comes
from the environment, not from what code is present.** So an arena is provisioned *with* an account,
the way it is provisioned with an os and arch, and the account becomes a third eligibility
dimension for any step that consumes model tokens.

**The account follows the OS user its runner runs as, and needs no new machinery.** A runner is
already [one per workspace](#deployment-topology--server-governor-runner) and long-lived, and it
runs as some OS user — so it picks up whatever credential that user's home directory or keychain
holds. Start each runner on a host under a different OS user and each draws on a different account.
That is the whole mechanism by which one machine runs several subscriptions at once: several
workspaces, several runners, several users.

The alternative — a single runner spawning steps as different users — would need privilege to switch
user on every spawn, which is both a permission surface and a way to attach the wrong credential
under load. Running the process *as* the user that owns the credential removes the question instead
of answering it.

Two existing pieces get load-bearing rather than incidental as a result.
[An arena record retained by its runner's presence](#a-host-that-is-merely-off-is-not-a-host-that-is-gone)
is untouched, since nothing about the runner's lifetime changes. And the **per-host verify lock**
now matters for a concrete reason: several runners on one box genuinely contend for that box's CPU,
which is the case that lock exists for.

### Quota is estimated, never known

Reactor's token count is an estimate. The provider is authoritative and may refuse mid-step, so
enforcement needs both halves and neither alone is sufficient:

- **Predictive** — do not dispatch a model-consuming step to an arena whose account is estimated
  spent. Cheap, and wrong at the margins.
- **Reactive** — when the provider refuses, mark the account `depleted until T` (the refusal
  usually says when), and fail the step.

The reactive half needs no new failure semantics: quota exhaustion is already an [infrastructure
failure](#infrastructure-failures-and-process-failures-are-different-things) by the standing test —
the work was never evaluated — so it retries unchanged, is not charged to the item, and does not
feed loop detection. Treating it as a process failure would blame an item for the fleet's budgeting.

### Scheduling — drain the perishable resource first

One asymmetry decides the policy. Subscription quota expires unused; API capacity does not.

> **Among eligible arenas, prefer the one whose subscription window resets soonest. Spill to an API
> account only when subscription capacity is exhausted, or when an item's latency is worth more than
> its cost.**

**Affinity beats drain.** That rule picks an arena for an item that has none; an item already
[bound to an arena](#an-arena-is-leased-to-an-item-not-to-a-step) stays there.

### Depletion makes an arena ineligible, not lost

The awkward case is an account depleting mid-resolution: the item is bound to an arena that holds
its accumulated state but can no longer think. The arena is neither healthy nor gone, and treating
it as either is wrong.

It is **temporarily ineligible**, and the choice is a cost comparison the deployment owner
parameterizes — window resets soon, so wait; resets far away, so revoke the binding and move,
accepting the loss of transient state; or spill that one item to an API arena. **No new mechanism is
needed**: depletion with a distant reset is another form of pressure that
[revokes a binding](#an-arena-is-leased-to-an-item-not-to-a-step), alongside capacity shortage.

### Two currencies, not one

Subscription work has near-zero marginal cost while consuming a scarce expiring resource; API work
costs real money and is elastic. **Metering them into a single number hides exactly the tradeoff
being managed**, so the ledger attributes both — tokens against an account's window, and spend
against a cap — per item, per step, and per account.

### Which pocket pays is not the step's business

A step should not know or care how its tokens are paid for. Payment is a **deployment** concern —
the same work is the same work whether it drew on a subscription window or a metered key — so
nothing about accounts appears in a step declaration or in the capability vocabulary.

**Reactor decides, from admin configuration.** The subscription is ambient to the arena and cannot
be withheld; the API key is injected into a step's process environment only when the deployment says
so. That asymmetry lands exactly on the risk boundary: the credential that costs money is the one
that can be withheld, and withholding it is credential scoping rather than a policy check, so a step
running without it cannot spend money whatever it does.

Putting this in a step grant instead would have coupled a project's flow definition to the
deployment's billing arrangement, and made every flow author responsible for a decision that is not
theirs to make.

### What phase one leaves out

Only subscriptions are implemented first. The eligibility filter, ledger attribution, and grant
vocabulary are identical for both kinds; only depletion differs — a resetting window against a spend
cap that does not reset. **One thing to confirm rather than design around:** provider terms differ
on whether subscription credentials may be used for automated or headless fleet workloads, and that
constrains how far the subscription-first path scales.

## Reliability — never stall, never spin

[Objective 2](#objectives) is "runs reliably unattended for prolonged periods." Unattended, that
decomposes into exactly two failure modes, and they pull in opposite directions:

> **Never stall.** Nothing waits on something that will never arrive. Every wait is backed by a
> live process and bounded by a deadline.
>
> **Never spin.** Nothing that costs tokens, money, or machine time runs twice unless the second
> run can do something the first could not. Every attempt must make progress in some form.

The first without the second gives an infinitely patient system that burns a quota re-running an
attempt that cannot succeed. The second without the first gives a thrifty system that silently
waits forever. Both are unattended failures with nobody there to notice, so both are invariants.

### Nothing runs unwatched

**Everything the runner causes to happen is a separate OS process with a pid** — a flow step, a
gate, a gate preflight, worktree setup, arena provisioning, a self-update swap. The runner performs
no work in its own address space; it spawns, watches, and reports. This is the operational half of
the [process-boundary argument](#seams-are-process-boundaries--by-design-not-by-accident): a
boundary only buys fault isolation, resource bounds, and killability if something is actually
holding the pid and watching it.

From that, a set of rules that admit no exceptions:

- **Every child is registered before it can do anything, and deregistered only on a reaped exit.**
  The registry entry carries the pid, the process start time, what the process is for (item, step,
  gate), its deadline, and the wait-state it backs.
- **"Waiting for X" is never a belief, always a pid.** Every waiting state in the runner and every
  in-flight lease on the server traces to a live registry entry. When the process behind it is
  gone, the wait ends — with a result if it exited, with a failure if it vanished. There is no code
  path where the process dies and the wait outlives it.
- **Every child has a deadline. There are no unbounded waits.** A step that declares no timeout
  gets the deployment default; a step that legitimately takes hours raises the number. "No timeout"
  is not a configurable value, because a hung agent is indistinguishable from a slow one and only
  the clock can tell them apart.
- **Liveness comes from the operating system, not from output.** Silence is not death and output is
  not progress — an agent can stream tokens forever while accomplishing nothing. The watchdog waits
  on the *process*; the deadline bounds it. Streamed output is telemetry. A heartbeat from the child
  would be an inference too; the process table is the only thing that is not a guess.
- **Kill the tree, not the child.** A flow spawns an agent, which spawns a compiler. Escalation on
  deadline is: graceful signal → grace period → hard kill of the whole process group → confirm
  reaped. A grandchild that survives is an orphan, and an orphan is a **reported fault**, not silent
  debris — an arena that accumulates them is a machine that stops working eventually.
- **Termination always produces a verdict.** Clean exit, non-zero exit, deadline kill, signal, or
  disappeared-without-status — each maps to a distinct, recorded step outcome. Nothing terminates
  into ambiguity, because an ambiguous outcome is what later becomes a stall.
- **A runner restart adopts nothing.** Pids recorded before a crash belong to a previous life: the
  runner no longer holds their pipes and cannot reconstruct their state. On start it kills what it
  recorded — matching pid *and* start time, so pid reuse cannot make it kill a stranger — fails
  those steps with a stated reason, and releases their leases.
- **The server assumes runners disappear.** Leases are time-bounded and reclaimable, the governor
  covers a dead runner, and nothing covers a dead machine — so no correctness property may depend on
  cleanup code having run. Every wait on the server side expires on its own.

### Every exclusion is held by a process, never by a flag

Serialization is where unattended systems die quietly. Anything that says *only one at a time* — the
per-project "an integration is in progress" lock, the per-host "verify is running here" lock, an
exclusive worktree, an arena assignment, a claimed item's lease — is the same primitive, and it
obeys one rule:

> **A lock is not a flag; it is a lease naming its holder as `(host, pid, process start time)`.**
> When that process is gone, the lock is gone. Releasing it is not a step the holder has to
> remember to perform.

That generalizes past locks to **every piece of persisted global state**. Each entry is in exactly
one of two forms — **held**, naming a process that is currently executing, or **timed**, carrying an
expiry that passes on its own — and there is no third form. Nothing persists on the strength of
having once been written, because a record that is neither owned nor expiring is one that only a
human can clear, and there is no human.

- **There is no "release" code path to get wrong.** The holder's *existence* is the lock. Explicit
  release is an optimization that returns the resource sooner, never the mechanism — because the
  one case that matters is the case where the holder never got to run its cleanup.
- **A lock nobody holds is not a lock.** A lock whose holder cannot be named as a live process is
  invalid on sight and is reclaimed. There is no "stale, probably still needed" state to reason
  about at three in the morning.
- **The start time is not optional.** `(host, pid)` alone is reusable — a rebooted machine hands the
  same pid to something else, and the lock silently transfers to an innocent process. The triple is
  what makes "is the holder still alive?" answerable rather than probable.
- **Reclamation is the server's job, not the holder's.** Every lease carries an expiry the holder
  renews while it lives. A holder that stops renewing loses the lock whether it crashed, was killed,
  lost the network, or had its whole host disappear — and a holder that was merely partitioned
  discovers on its next renewal that it no longer holds the lock, and must stop rather than assume.
- **Every reclamation is recorded.** A lock taken away from a dead holder means work was interrupted
  mid-flight; that fact belongs in the ledger with the holder's identity and the reason, not
  silently swept up. It is also the honest signal that something is crashing repeatedly.

This is the same discipline as [nothing runs unwatched](#nothing-runs-unwatched), applied to state
instead of execution: a wait is backed by a pid, and so is a lock. A deployment where one crashed
process wedges the whole fleet behind a stuck integration lock is exactly the unattended failure
objective 2 exists to rule out.

#### Exclusions are declared, and waiting for one is not work

The lease model above says how a lock is *held*. It does not say who asks for one, or what the clock
does while they wait — and both matter, because a step is time-bound and an exclusion is by
definition contended. This is [invariant
3](base-engineering.md#3-serialization-is-declared-and-waiting-for-it-is-not-work); Reactor's half
of it is three mechanisms.

> **Every step and every gate declares its exclusions statically. Queue time is charged to a
> separate clock from work time.**

- **Two deadlines, not one** (distinct from the two *lease* clocks
  [below](#a-host-that-is-merely-off-is-not-a-host-that-is-gone)). The declared `timeout` bounds
  *work*; a separate **queue deadline** bounds how long a step may wait to start. Charging queue
  time to the work deadline makes a timeout a function of fleet load, so steps begin failing under
  contention — precisely when the system is busiest and a false failure is most expensive. Exceeding
  the queue deadline does not fail the step: it returns to the queue, recorded as **contended**,
  which is a capacity signal rather than a defect. Two deadlines are also what keeps this from
  reintroducing the unbounded wait that [never stall](#reliability--never-stall-never-spin) forbids
  — excluding queue time from one is safe only because the other is still running.
- **Static declaration, canonical acquisition order.** Because the set is known before dispatch,
  Reactor acquires in a total order over lock names and can reject an unsatisfiable set up front.
  Locks discovered at acquisition time cannot be ordered, and unordered acquisition of more than one
  lock is a deadlock that waits for load to find it. This is the same shape as the static
  role-versus-flow check: declared data admits a check that runtime discovery does not.
- **The set is transitive and computed.** A step that runs a gate inherits that gate's
  `serialized_by` — the per-host verify lock, an exclusive worktree, a resource cap like `host:cpu`.
  The effective set is the step's own union everything it invokes, derived from the manifest rather
  than hand-listed on the step, or the two declarations drift and the ordering guarantee is lost.
- **A name is `<scope>:<leaf>`, and only the scope is Reactor's business.** Scope is drawn from a
  closed set — `project`, `host`, `arena`, `global` — and the leaf is opaque, so a project may
  invent `project:migration` without a change to the shared layer. Reactor resolves `project:`
  against the item's project, so two projects never contend on the same leaf, while `host:cpu`
  contends across everything on that box regardless of which project caused it. **The scope is what
  makes the name meaningful to the scheduler**: opaque strings would force Reactor to treat every
  exclusion as global and serialize unrelated projects against each other. Ordering is separate and
  easier — any total order over names works, so lexicographic suffices.
- **`host:` resolves to the physical machine, and everything on it resolves the same way.** A
  sandbox's arena inherits its parent machine's host identity rather than minting its own, and two
  governors [splitting supervision by OS user](#deployment-topology--server-governor-runner)
  resolve to one machine between them. What `host:cpu` and the per-host verify lock arbitrate is
  hardware, and **hardware does not multiply when you virtualize it or when you add a supervisor.**
  Getting this wrong fails quietly: every lock looks correctly held while the machine is
  oversubscribed by the number of sandboxes on it.
- **Arena count is therefore not parallelism.** A box may hold eight arenas and still run one
  verify at a time. That is correct rather than a scheduling defect — arenas are cheap and the
  machine is the scarce thing — but it means throughput follows a host's physical exclusions, not
  its arena count, and a fleet sized by counting arenas is sized wrong.

**Waiting is still a watched state.** A step blocked on an exclusion has a live process and a
registry entry like any other; "waiting for the integration lock" traces to a pid and a queue
deadline, never to a belief. What changes is only which clock is charged.

#### An arena is in exactly one of four states

The lease rule above names a holder for every exclusion, and the arena model does not yet hold up
its end. It names one only for an *item* — so everything else that occupies a machine takes it
invisibly: a scheduled gate run, a discovery or bisect job Reactor starts for itself, a person
working directly on their own laptop. An arena forty minutes into a monitor has no item and no step
in progress, which under [demand-driven reclamation](#an-arena-is-leased-to-an-item-not-to-a-step)
makes it the *most* attractive victim in the pool. That is the same defect
[trunk-red preemption](#gate-execution--reactors-half) fixes for the integration lock by giving it a
real holder, left standing here: occupancy owned by nothing is a flag.

> **An arena is always in exactly one of four states — `leased`, `reserved`, `idle`, or
> `offline` — and it holds at most one lease at a time.**

The four states describe an **adopted** arena; a host that has announced itself and not yet been
adopted is not an arena at all and holds nothing
([above](#a-host-is-not-an-arena-until-it-is-adopted)).

| State | Held by | In the pool |
|---|---|---|
| **leased** | one work unit — an item resolution, a gate run, or a job Reactor runs for itself | no |
| **reserved** | a named person, for direct work | no |
| **idle** | nobody | yes |
| **offline** | whatever it held when its runner stopped renewing | no |

- **One lease at a time; concurrency is more arenas, not more leases.** An arena is one workspace
  with one runner, so "two things at once on this box" is already expressed as two workspaces under
  two OS users — the mechanism [model accounts](#the-account-belongs-to-the-arena-and-that-is-forced)
  depends on. Reusing it keeps occupancy answerable: *what is this arena doing* has exactly one
  answer, and `host:cpu` remains the exclusion that arbitrates the real contention between them.
- **Three kinds of lessee, differing only in stickiness.** An item's lease is **sticky** — taken
  at first dispatch and held across steps, because [the accumulated state is what makes the next
  step cheaper](#an-arena-is-leased-to-an-item-not-to-a-step). A gate run's and a Reactor job's are
  **transient**: they end when the process ends. A gate is built from the commit under test, on a
  fresh worktree with its own preflight, so nothing accumulates that a later run would want and
  stickiness would protect nothing. Same primitive either way; what differs is what releases it.
- **A transient lease is not a victim.** Under capacity pressure the scheduler waits for it rather
  than revoking it. Killing a gate run mid-flight destroys the whole run — there is no partial
  result and the next attempt starts from the beginning — while the deadline it already carries
  bounds how long pressure has to wait. Sticky bindings remain the victims, in the order
  [already stated](#an-arena-is-leased-to-an-item-not-to-a-step).
- **`reserved` is a state, not a note on an idle arena.** A developer's machine is theirs first and
  the fleet's second; for that class of arena `reserved` is where it sits most of the time, and the
  fleet borrows it rather than the other way round. Modelling that as policy layered on `idle`
  means the scheduler reads an occupied workstation as free capacity and the person contends with
  the fleet for their own CPU. A reserved arena is still adopted and registered — renewing
  presence, reporting health, and dispatchable explicitly by the person holding it. It is simply
  not in the pool.
- **An arena is born in its host's default state, and a lapsed reservation returns it there.** Not
  to `idle`: reverting unconditionally to the pool would let a workstation's closed default expire
  away, which is the one thing that default exists to prevent. One rule covers both machines — on
  a build box, a developer who grabs a workspace and forgets about it releases it back
  automatically; on a laptop, a lapsed reservation simply re-reserves and the machine stays theirs.
  Arenas Reactor [provisioned itself](#a-host-is-not-an-arena-until-it-is-adopted) are the exception
  in the same way they are for adoption: the server made them for a purpose, so they come up
  working.
- **A reservation is timed, because a person is not a process.** `(host, pid, start time)` cannot
  name a human holder, so the [held-or-timed rule](#every-exclusion-is-held-by-a-process-never-by-a-flag)
  leaves only the other form: a reservation carries an expiry its holder extends, with a deployment
  default. Without one, somebody who reserves a machine and then goes on leave removes it from the
  pool permanently and silently — precisely the record only a human can clear that the rule exists
  to forbid. Expiry hands the arena back to its host's default rather than to the pool, per the
  bullet above. The deployment owner may also revoke, recorded like any other reclamation.
- **`offline` retains; it does not release.** An offline arena keeps what it held, and the two
  clocks [below](#a-host-that-is-merely-off-is-not-a-host-that-is-gone) decide the rest: a transient
  lease dies with its process within minutes and its work is placed elsewhere immediately, a sticky
  binding waits on the short unreachability clock, and the arena record is retained on the long one
  before the arena is declared lost. A reserved arena that goes offline stays reserved — a closed
  laptop is the normal case, not a signal — until its own expiry passes.
- **Only `idle` accepts a new lease, which makes monitor cadence best-effort.** A fleet fully leased
  to items cannot place a scheduled gate, and a cadence the scheduler cannot keep should not be
  presented as one. A monitor that misses its window is recorded as **contended**, exactly as a
  [queued step](#exclusions-are-declared-and-waiting-for-one-is-not-work) is — a capacity signal
  rather than a defect — and a gate that is repeatedly contended is a fleet too small for its own
  quality floor, which is worth being able to see.
- **Every transition is recorded, with its cause.** Occupancy history is what answers *why did
  nothing run on this machine for six hours*, and it is the same accounting that
  [write-offs](#a-host-that-is-merely-off-is-not-a-host-that-is-gone) and repeated interruptions
  feed: a machine whose time goes to contention, reservation, or absence rather than to work is
  only visible if the states are counted.

#### A host that is merely off is not a host that is gone

One expiry cannot serve both cases. A runner that vanishes mid-step is blocking the fleet *now*; an
arena whose machine is closed for the night is not blocking anything and will very likely be back.
Reaping both on the same clock forces a choice between a fleet that wedges and a fleet that
reprovisions itself every time somebody shuts a lid. So there are two clocks, and they differ by
orders of magnitude:

| | Renewed | Expires in | On expiry |
|---|---|---|---|
| **Work leases** — item claims, a transient arena lease, a per-project integration lock, a per-host verify lock, an exclusive worktree | continuously, by the holding process | seconds to minutes | claims return to the queue, locks release, the step is failed and recorded |
| **Arena retention** — the arena record: its identity, provisioned state, its assignment to a project, and whatever it currently holds (a lease or a reservation) | by the runner's presence | hours (**default 24**, deployment config) | the arena is **declared lost** |

**Work never waits on a returning host.** The moment a runner stops renewing, everything it was
holding is reclaimed and its items are dispatchable again — the long clock applies only to the
*retention* of the arena record, never to the work. Otherwise the second clock would reintroduce
exactly the stall the first one exists to prevent.

**One bounded exception: an item whose [bound arena](#an-arena-is-leased-to-an-item-not-to-a-step)
has stopped responding while it has work to do.** Its accumulated state lives on that arena, so
dispatching it elsewhere does not rescue the work — it silently discards it. Such an item waits, on
a **third lease clock, much shorter than retention**: on expiry the binding drops, the
transient state is written off, and the item is dispatchable anywhere from its last commit. This
clock exists only for unreachability — an item merely *idle* on a healthy arena is not waiting on
anything and is never evicted by time. Holding an item for a day to save twenty minutes of agent
work is the wrong trade, and the number should say so. This is a wait, not a stall, by the same test
as [parking](#every-attempt-must-make-progress) — bounded, recorded, and with a stated reason — but
it is the one place where work waits on a host at all, so it is called out rather than left
implicit. A step declared *arena-independent* is not subject to it and dispatches immediately
elsewhere.

**What "declared lost" means is deliberate.** It is the terminal transition out of `offline`, not
a synonym for it and not "degraded" — the arena record is force-dropped, the capacity returns to
the pool, and an ephemeral arena is reaped at its provider. The state on it is written off, not
awaited.

- **Anything on a lost arena is gone.** Uncommitted worktree state, local caches, partial artifacts,
  output that was never streamed. There is no "it might come back with the work still in it", and
  treating the arena as a possible source of truth later is what turns a temporary absence into
  permanent corruption. **Arena-held state is an optimization, never authority**: it makes the next
  step cheaper and an interruption survivable — see [an arena is leased to an
  item](#an-arena-is-leased-to-an-item-not-to-a-step) — but every fact a correctness decision rests
  on must already have been streamed to the server or committed. Losing an arena therefore costs
  work, never truth, and that is what makes the write-off safe rather than merely unavoidable.
- **A host that reappears after being declared lost is a new arena.** It registers again — its
  host is still [adopted](#a-host-is-not-an-arena-until-it-is-adopted), so no operator is needed —
  is issued a fresh arena record, reprovisions from scratch, and resumes nothing. Any lock it
  believes it holds already belongs to somebody else, so it must re-acquire before touching
  anything — a returning runner that trusted its own memory would be a second writer against state
  that has moved on without it.
- **The write-off is a ledger record, not a log line.** It names the arena and its host, how long it
  was absent, which leases, items, and artifacts died with it — including any gate run or Reactor
  job that held it transiently, which belong to no item and would otherwise be written off with no
  record at all — and what had already been spent on them. That record is what lets the affected
  items be requeued with honest history instead of reappearing as mysteries — and a host that
  accumulates these is a machine to take out of the pool, which is only visible if the losses are
  counted.
- **The threshold is the deployment owner's, not the project's** — it belongs in
  [ConfigStore](#configstore--the-deployment-owners-residual) with the rest of arena allocation. A
  CI arena farm may want thirty minutes; a fleet of developer laptops wants to survive a long
  weekend.

#### An arena is leased to an item, not to a step

"Anything on a lost arena is gone" is the right rule for an arena that is *lost*. Applied between
steps, it would throw away the state that makes a resolution cheaper as it proceeds — the agent's
on-disk session and notes, scratch files, a warm cache, the materialized worktree — and would make
an interrupted step unrecoverable, since re-running it from its starting commit can do nothing the
first attempt could not. That write-off does not merely lose work; it **spins**.

None of that state can be reliably captured and moved — see [invariant
4](base-engineering.md#4-an-items-work-binds-to-an-arena-and-carries-its-state-forward), whose
decisive argument is that capture is cooperative code while the terminations that matter are the
ones where no code on that host runs at all. So the state stays put and the work goes to it:
**Reactor binds an item to an arena at first dispatch and keeps the binding for the whole
resolution.** Interruption is the degenerate case of that rule, not a mechanism of its own.

Reactor's half is four obligations:

- **`item → arena` is first-class persisted state.** The scheduler dispatches the item's steps to
  that arena; dispatching elsewhere silently discards accumulated state, which is worse than
  refusing outright.
- **The lease is sticky: it stays with the item rather than returning to the pool between steps.**
  This is the ordinary arena lease of [the state model](#an-arena-is-in-exactly-one-of-four-states),
  held across steps rather than released at each process exit — the *work* lease still dies with
  each step's process, so [nothing runs unwatched](#nothing-runs-unwatched) is untouched and every
  step is an ordinary new child watched like any other.
- **Step declarations can relax the binding, and Reactor must honor both forms.** A step declaring
  *arena-independent* may be dispatched anywhere, freeing capacity; a step declaring *fresh session*
  runs on the bound arena but is launched with no inherited agent context. The second is an
  integrity requirement where consecutive steps differ in trust — see [invariant
  4](base-engineering.md#a-step-may-declare-that-it-does-not-want-the-inheritance) — so it is
  enforced at launch, not requested of the agent.
- **The binding is released by demand, not by a clock.** An idle arena costs nothing while the pool
  has spare capacity, and breaking a binding always costs something — the transient state is gone
  and the next step starts from the last commit. Those are not symmetric, so a healthy binding gets
  **no expiry timer**: it is reclaimed when the pool is genuinely short and otherwise left alone,
  however long the item sits. Under pressure, prefer victims whose item has no step in progress,
  then least-recently-used, and record the reclamation like any other. A
  [depleted model account](#depletion-makes-an-arena-ineligible-not-lost) whose window resets far
  away is the same kind of pressure and revokes by the same path.
- **A binding protecting nothing is released without waiting for pressure.** Demand-driven does not
  mean cling by default. When an item [blocks on another
  project](#blocked-is-a-recorded-state-not-a-stall) at a clean step boundary, the arena holds only
  warm cache, and releasing costs nothing — so it releases immediately rather than idling until
  someone else needs the capacity.

**The binding is revocable, not a lock.** While it stands the scheduler honors it — that is the
point of recording it — but the server may drop it at any moment, and dropping it is an explicit,
recorded act with a reason (capacity pressure, or the unreachability clock above), never a silent
reroute. Revocability is also what keeps the binding from becoming the orphaned state [every piece
of persisted state is held or timed](#every-exclusion-is-held-by-a-process-never-by-a-flag) exists
to forbid: an entry the server may unilaterally clear needs no expiry to avoid outliving its
purpose, which is how it can have no timer without becoming the third form that rule denies.

**A runner restart adopts no processes, but disk is not a process.** The restart rule kills recorded
pids, fails their steps, and releases their *work* leases — it does not erase the worktree, and it
must not drop the arena's binding to the item either. Those are different lifetimes on the same
host, and conflating them turns a survivable interruption into a write-off.

**When the arena really is lost, the item restarts its current step from the last commit.** That is
the accepted floor and the reason commits still matter, but it is a different event from a resume —
one starts with the partial tree and the agent's notes, the other with neither — and the ledger must
distinguish them, because only the second is a repeat.

### Infrastructure failures and process failures are different things

Everything that can go wrong falls into one of two classes, and conflating them is how an
unattended system either spins forever or fills its backlog with items marked failed that were
never actually tried.

> **Infrastructure failure** — the model API is down, the model quota is exhausted, GitHub is
> unreachable, the arena was preempted, the network dropped, a disk filled. The *work* was never
> evaluated. These say nothing whatsoever about the item.
>
> **Process failure** — a gate failed, the code did not compile, the agent produced nothing usable,
> a conflict could not be resolved, a step exceeded its deadline. The work *was* evaluated, and this
> is the answer.

**The distinguishing test is not "is it transient?" but "did the work get evaluated?"** A process
failure is a result and is recorded as one. An infrastructure failure is the absence of a result,
and recording it as one is a lie the system then acts on.

Everything else follows from that:

- **An infrastructure failure is not the item's fault, and must not be charged to it.** It does not
  consume the item's attempt budget, does not feed [loop
  detection](#every-attempt-must-make-progress), does not mark the item failed, and does not park it
  for a human. Otherwise a two-hour GitHub outage quietly burns down every item's retry budget and
  leaves a backlog of items that look tried and were not.
- **They are handled fleet-wide, not per item.** Quota exhaustion and a downed dependency are
  properties of the *deployment*, so the scheduler stops dispatching everything that needs that
  dependency and probes it from one place. Letting N items each independently rediscover the same
  outage is itself a [never-spin](#every-attempt-must-make-progress) violation — N times the cost
  for one piece of information. Backoff and the circuit breaker are shared, not per item.
- **Quota exhaustion is a scheduled resumption, not an error at all.** The reset time is usually
  known; the right response is to pause dispatch until it and then continue, not to retry into a
  wall or fail the work that was in flight.
- **Infrastructure failures are resumable at the step boundary.** The step is already the unit with
  a declared grant and a resolved flow version, so resumption re-runs the *step*, not the item —
  which is what "interrupted work resumes rather than restarts" means concretely. It puts a real
  requirement on flows: **a step must be re-runnable from its declared inputs**, and any step with
  an external side effect (a push, a PR creation, a merge) must be idempotent by construction —
  keyed on the item and check-then-act against the API — because the failure that interrupted it may
  well have landed after the effect and before the acknowledgement.
- **Classification happens where the failure is raised, never by pattern-matching a message later.**
  The error carries its class from the point of origin; a string-matched taxonomy reclassifies
  itself the first time an upstream error message changes wording.
- **An unclassified failure is treated as a process failure.** The two mistakes are not symmetric:
  calling a process failure "infrastructure" retries it forever, while calling an infrastructure
  failure "process" costs a human one look at an item that stopped. Default to the one that stops.
- **A deadline kill is a process failure**, for the same reason. A hung step might have been blocked
  on a sick dependency, but it might equally be an agent looping, and treating timeouts as
  infrastructure gives the one failure mode that *always* recurs an unlimited retry budget. A step
  that hangs because a dependency is down will be caught by the fleet-level pause anyway, from the
  side that can actually tell.

Note that neither class needs the *holder* rules relaxed: an infrastructure failure that takes out
the runner still releases its [leases](#every-exclusion-is-held-by-a-process-never-by-a-flag) and
still requeues the item. Being resumable is about not losing the work, never about holding a lock
across the outage.

### Every attempt must make progress

The rule, and it applies to any work that costs tokens, arena time, or money:

> **A retry must differ from the attempt it repeats** — different input, different tree state,
> different flow code, or a narrowed problem. Repeating an identical attempt against identical
> state is a bug, not resilience.

- **The two failure classes retry differently, and that is the whole point of separating them.** A
  process failure is *never* retried unchanged: it either escalates with the failure itself as new
  input, or it stops. An
  [infrastructure failure](#infrastructure-failures-and-process-failures-are-different-things) *is*
  retried unchanged — and it does not violate the rule above, because what differs is the state of
  the world rather than the work, and the attempt it repeats spent nothing on the work to begin
  with. What keeps that honest is that the retry is bounded, backed off, and coordinated
  fleet-wide rather than per item.
- **Every step's cost is metered and attributed** to item and step: tokens, wall time, arena time.
  Work that is not metered cannot be budgeted, and work that cannot be budgeted cannot be stopped.
- **A work-hour is a metered agent-hour**, and it is the unit the
  [engagement feed](engagement-feed.md#ranking--regret-per-minute-of-attention) ranks in — so what
  is at risk behind a blocked item has to be a quantity, not a phrase:

  > **Work at risk behind a blocker = Σ over the items it blocks of *work already sunk* plus *work
  > still estimated to remain*.** The first is measured from the ledger. The second is estimated.
  > They are summed for ranking and **never merged in the record**.

  Both are needed and neither substitutes: sunk work is what a wrong or abandoned decision throws
  away, while for some items the work still ahead dwarfs it, and a model that saw only what had
  already been spent would rank a nearly-finished item above one that has barely started and has a
  month left.

  - **Sunk is measured** — the wall and arena time already attributed to those items. No new
    metering; it is the same ledger the budget reads.
  - **Remaining is estimated**, from what items of that kind have historically cost to complete.
    **Weighted by the evidence behind it**, so a kind with no history contributes almost nothing and
    earns weight as samples accumulate — the same posture as a
    [ratcheted baseline](base-engineering.md), which tightens as evidence arrives rather than being
    trusted on day one.
  - **A declared estimate is a bounded adjustment, not an input.** A step estimating the work its
    own question unblocks is the constrained party sizing its own priority, so it is discounted per
    source exactly like a declared impact.
  - **Sunk work becomes more at risk the longer it waits**, because trunk moves under it and
    [reconciling with a moved trunk is creative work rather than a mechanical
    rebase](#why-the-step-grant-matters-even-for-a-fully-trusted-actor). That drift term is what makes [blocked items surfacing
    by age](#blocked-is-a-recorded-state-not-a-stall) a mechanism rather than an aspiration — without
    it, a question blocking one item for a month ranks on day thirty exactly as it did on day one.

  **The components are displayed, never just the sum** — *"11 items blocked · 43h spent · ~120h
  estimated remaining"* — for the reason [two currencies](#two-currencies-not-one) gives: a single
  number hides which half is known and which is guessed.
- **Budgets are grants that escalate, not ceilings that stop.** A fixed budget answers "how much
  should this cost" — badly, since nobody knows in advance — and then gets used to answer "is this
  still making progress", which it cannot answer at all. A genuinely hard item and a thrashing one
  look identical to a spend counter, so a fixed budget stops the wrong ones, and repeatedly
  stopping and restarting work that was progressing is itself a major source of instability. See
  [the grant ladder](#the-grant-ladder) below.
- **Budgets are metered in two currencies.** Subscription tokens and API spend are not
  interchangeable — one is a scarce resource that expires, the other is money — so a grant names
  both rather than collapsing them (see [Model accounts](#two-currencies-not-one)). Quota
  exhaustion pauses rather than spinning against a limit.
- **A block is not a loop if it carried work forward.** A step that blocks produces no verified
  artifact, so the predicate above would class every legitimate block as a spin. The discriminator
  is whether its [checkpoint](base-engineering.md#a-step-may-carry-work-forward-without-claiming-completion)
  advanced: work done and recorded is progress that cannot finish yet, while a block whose
  checkpoint stood still repeats an attempt that already failed.
- **Loop detection is a first-class state.** The same step, on the same input digest, failing with
  the same signature N times means the item is *stuck* — it stops being dispatched and is surfaced.
  Stuck and known is a fine state; stuck and busy is precisely the failure this rule exists to
  prevent.
- **A resume is not a retry, and the counter must know the difference.** A step
  [resuming on its bound arena](#an-arena-is-leased-to-an-item-not-to-a-step) starts from the
  partial tree and the agent's own notes, so it can do what the interrupted attempt could not and
  does not count toward the rule above. A restart from the last commit after the arena was lost
  starts from nothing extra, and does. Counting both as "attempt N+1" is how a system ends up parking
  work that was progressing or re-running work that never will. **Repeated interruption is still a
  signal, just not about the item** — a step killed and resumed many times on one arena indicts the
  arena, and belongs in the same health accounting as accumulated write-offs.

### Waiting on a person

"Every wait is backed by a live process and a deadline" is a rule about processes, and it does not
describe a [`waiting` or `parked` hold](#the-states-and-what-they-belong-to): no pid is running, and
the whole point of some of them is that no clock ends them. Rather than carve an exception, state
the human half as a peer:

> **Every wait on a process is backed by a live pid and a deadline. Every wait on a person is backed
> by an escalation ladder that terminates.**

The ladder, in order, and each rung only if the one before it produced nothing:

1. **Addressed** — a named principal, if the question named one.
2. **Role** — the preference [lapses](engagement-feed.md#audience-and-tags) and the whole answering
   role sees it.
3. **Escalated** — the window elapses and the audience widens up the trust ladder, each rung with
   its own window, terminating at the role
   [guaranteed to exist with a live principal behind it](engagement-feed.md#four-rules-that-close-the-remaining-gaps).
   That guarantee is what makes the ladder finite instead of hopeful. **Widening is the one moment
   the system pushes** rather than waiting to be visited — see
   [the feed pulls, escalation pushes](engagement-feed.md#the-feed-pulls-escalation-pushes) — because
   including someone new only helps if they find out.
4. **Defaulted** — the ladder is exhausted and the question carries a recommendation: it fires,
   recorded as `defaulted` with the ladder's history attached.
5. **Permanent wait** — the question carries no default because an answer is genuinely required. It
   waits, visibly, owned by the role that must answer it, and
   [rising](engagement-feed.md#ranking--regret-per-minute-of-attention) as it ages.

**"Couldn't ask" and "asked and nobody answered" are different, and only one may default.** Silence
from someone who saw the question is information — the recommendation was put in front of them and
not contradicted. Silence from someone the question never reached is not, which is why
[chronic non-delivery converts a defaulted question to a pinned one](engagement-feed.md#the-clock-runs-on-delivery-not-on-creation)
rather than firing it. The distinction looks like a contradiction and is the opposite of one.

**Rung 5 is a real state and it should be rare.** Every question that can carry a defensible
recommendation should carry one, because a permanent wait converts the fleet's throughput into one
person's response time. Where an answer truly is required — an irreversible decision, a change to
intent, something no recommendation can honestly propose — waiting is correct, and it is not a stall
by the same test as everything else here: it is recorded, owned, visible, and endable by a named
person. A stall is a wait nobody can see and nothing can end.

**Parking is not stalling either**, for the same reason. Deciding that an item cannot progress
without a human, and saying so, is progress. Continuing to spend on it is not.

### The grant ladder

A spend limit and a runaway detector are different instruments, and using one for both is what makes
a fleet stop work that was fine while letting work that was doomed run to the cap. Separate them:
**the grant is a ceiling; the decision to extend it is a progress judgment made with evidence that
did not exist when the item started.**

> **Work inside its grant proceeds without asking. Work that would exceed it requests an extension,
> which is decided rather than assumed. Above a hard ceiling, only a human may grant.**

Three rules keep that from becoming a slower runaway:

- **Extend only on evidence of progress.** A ladder with no progress predicate is a budget with
  extra rungs, arriving at the same runaway later and more expensively. The predicate is
  [invariant 6](base-engineering.md#6-a-steps-completion-is-a-verified-artifact): *has a new
  verified artifact appeared since the last grant?* — not "has it spent less than X". That is the
  same distinction [a resume is not a retry](#every-attempt-must-make-progress) draws for
  interruption, applied to spend: what counts is whether this attempt could do something the last
  one could not.
- **Extension is a capability, not a policy setting.** `budget.extend:<limit>` is granted per role
  like everything else: an automated policy extends to one bound, an admin further, and past the top
  only a human. An extender that can raise its own ceiling is not a ceiling — the same
  self-authorizing failure the whole authority model is built to exclude, and it applies with full
  force here **because anything smart enough to judge a runaway is smart enough to cause one**.
- **The hard ceiling is the deployment owner's**, held in
  [ConfigStore](#configstore--the-deployment-owners-residual), out of reach of everything it
  constrains.

**The grain is the step run**, because that is the smallest unit where the progress question is
answerable — this run either produced a verified artifact or it did not. Per-item budgets remain, as
the outer bound; they are not fine enough to tell a hard step from a stuck one.

### Running out is a wind-down, not a kill

Terminating a step the instant its grant is exhausted is how a system loses the work it already paid
for. Anything the agent had not yet written durably is gone, and the tree can be left half-changed —
so the cheapest moment to stop is also the moment with the most to lose.

> **At exhaustion: ask for an extension. If refused, instruct the step to wind down, with a reserved
> allowance to do it. If it does not stop within the grace period, escalate to a hard kill.**

- **Wind-down has a narrow meaning, and it is not "finish the task."** It is *reach a verifiable
  artifact* — [invariant 6](base-engineering.md#6-a-steps-completion-is-a-verified-artifact) — or
  failing that, leave the tree in a state the next attempt can start from. That is achievable in a
  bounded window in a way "complete the work" is not.
- **The wind-down allowance sits outside the grant**, because otherwise the instruction is
  unfundable: an agent cannot be told to spend nothing and also write down what it did. A small
  reserve, spent only on stopping, is what makes the graceful path exist at all.
- **The hard kill is not optional.** A step that will not stop is
  [escalated exactly like a deadline](#nothing-runs-unwatched) — graceful signal, grace period, hard
  kill of the process group, confirm reaped — because a wind-down that can be ignored is a
  perpetual stall wearing a politer name. **Never-stall outranks the work.**
- **What the kill costs is bounded by the arena**, not by the grant. The worktree and everything
  else the step accumulated stay where they are under
  [invariant 4](base-engineering.md#4-an-items-work-binds-to-an-arena-and-carries-its-state-forward),
  so a killed step is resumable rather than lost. That is what makes the hard kill affordable to
  reach for.

The order matters: extension first, wind-down second, kill last. Reversing the first two spends a
human decision on a step that only needed thirty more seconds, and skipping the second is the
current behaviour that loses work.

**A grant is not a quota, and the two must not be conflated.** A grant bounds what *this item* may
spend and is a judgment about the work; an account's [quota](#quota-is-estimated-never-known) bounds
what the *deployment* has left and is a fact about the world. Running out of grant asks whether to
extend; running out of quota is an
[infrastructure failure](#infrastructure-failures-and-process-failures-are-different-things) that
resumes on its own when the window resets. Treating the second as the first would ask a human to
approve spending money that does not exist.

An item waiting on a human grant is **waiting**, not parked — nothing has gone wrong, a person is
simply an input ([the states](#the-states-and-what-they-belong-to)). It is recorded, owned, visible,
and not spending, and it needs no mechanism of its own: an extension request *is* a question, with a
required role and a window like any other.

## Step execution — Reactor's half

The flow-facing contract is
[Step resolution](base-engineering.md#step-resolution--steps-dispatch-themselves). Reactor drives
the loop, and four obligations fall to it.

1. **Scan, do not plan.** For the item's role and type, walk the declared steps calling `check`,
   `run` the first `unsatisfied` one, then re-enter the scan. Nothing is precomputed and nothing is
   cached between dispatches, because the flow version may have changed under the item since the
   last one.
2. **`check` is launched without a model account.** The absence of the credential is the
   enforcement; a `check` that wanted to spend could not. `run` gets the arena's ambient
   subscription and, if the deployment allows,
   [an injected API key](#which-pocket-pays-is-not-the-steps-business).
3. **The grant is per `run`.** That is the grain at which the
   [ladder](#the-grant-ladder) asks its question, because a run either produced a verified artifact
   or it did not. A `check` costs no grant, which is the other half of why it must be cheap.
4. **Record every scan decision, not just the step that ran.** This is the cost of removing the
   plan and it must be paid deliberately.

**On that last point.** A stored plan is a thing you can look at and ask *why is it doing that*.
Self-dispatch replaces it with a path that exists only in hindsight, so the ledger becomes the only
account of what happened: for each scan, every step consulted, its verdict, and the reason. Without
that, a wrong traversal is unreproducible and the model's flexibility becomes its own failure mode —
the same trade the design accepts elsewhere by insisting that
[nothing terminates into ambiguity](#nothing-runs-unwatched).

**Outcomes route to machinery that already exists.** `blocked` becomes a
[blocking edge](#an-edge-names-a-target-and-a-condition-never-a-version) or a park, and releases the
arena binding by the [clean-boundary rule](#an-arena-is-leased-to-an-item-not-to-a-step) since the
reporting step completed. `handoff` makes the item eligible for a role that can run the next step
and is recorded as such — never as completion, which would drop the work, and never as blockage,
which would point remediation at a blocker that does not exist. `complete` finalizes.

**One property to preserve from the model this replaces.** Its fixed budget-per-artifact is what
stopped every runaway from becoming unbounded — a bound worth keeping even though the instrument
changes. The [grant ladder](#the-grant-ladder) is that bound, re-expressed: a scan that re-resolves
until satisfied, with no per-run grant behind it, would be a reliability regression however much
cleaner it reads.

## Gate execution — Reactor's half

The project declares its gates; **Reactor discovers, schedules, and executes them.** Reactor's
responsibilities end at the manifest boundary:

1. **Discover.** Run the project's gate-listing command in a registered worktree, validate the
   manifest, and create a `Gate` record per entry — merging any existing deployment-side overrides
   keyed by name. Re-run on new commits and on a slow refresh tick; new gates adopt with defaults,
   removed gates retire (history preserved), and changed metric semantics flag for admin review
   rather than silently invalidating baselines.
2. **Schedule.** Two invocation modes, not one. **Monitors** are picked per host OS × arch ×
   deployment overrides, honoring each gate's declared cadence, and placed only on an
   [`idle`](#an-arena-is-in-exactly-one-of-four-states) arena — so a cadence is a target rather
   than a promise the pool can always keep, and a missed window is recorded as **contended**.
   **Preconditions** are not scheduled at all — they are selected by the transition attempted
   (`blocks`) and run on the host attempting it, inside the lease the attempting step already holds,
   since that step [inherits the gate's
   exclusions](#exclusions-are-declared-and-waiting-for-one-is-not-work). A gate may be both.
3. **Execute.** **Take a transient arena lease**, acquire the gate's `serialized_by` exclusions in
   canonical order, run the declared preflight on a fresh worktree, then the gate command as a
   subprocess, parse the JSON output envelope from stdout, and write results to `LedgerStore`. The
   lease is released when the process exits: a monitor run is a work unit like any other, and
   differs from an item's only in carrying nothing forward. **A gate run that occupied a machine
   without holding it would be exactly the flag [the lease
   rule](#every-exclusion-is-held-by-a-process-never-by-a-flag) forbids** — invisible to capacity
   pressure, to victim selection, and to the write-off ledger.
4. **Retain deployment-side config**, keyed by `(project, gate_name)` and layered *on top of* the
   manifest: arena assignment (the project says "I need linux/amd64"; Reactor decides *which*
   linux/amd64 arena), manual overrides (disable, narrow host match, force a cadence, adjust a
   ratchet cap, downgrade a metric during an incident, grant temporary exceptions), and metric
   history / baselines / ratchet state.

**Layering rule: the manifest defines the contract; deployment overrides constrain or annotate
it.** Overrides never *add* metrics or change a metric's direction — those are gate-contract
concerns owned by the project. Reactor never silently invents fields the project didn't declare.

**Trunk red preempts.** [Invariant 1](base-engineering.md#1-origin-is-always-green-on-every-platform)
requires that a cross-platform failure be *undone*, not merely filed, so a monitor failing on trunk
is not an ordinary auto-filed bug: Reactor **files a repair item, and that item holds the project's
integration lock** until green, dispatched ahead of the queue. Giving the lock a real holder matters
rather than being bookkeeping: a lock held by a *state* is a flag, which
[the lease rule](#every-exclusion-is-held-by-a-process-never-by-a-flag) forbids precisely because
nothing releases it when the process that noticed dies. It also resolves what would otherwise be a
deadlock — the repair is the one thing that *can* integrate, because it holds the lock rather than
being excluded by it — and it makes "why is nothing landing" a question with a clickable answer.

A repair that parks for a human freezes that project's landings. That is the correct behaviour, not
a flaw: building on a known-red trunk is the cascade the invariant exists to stop. It must be
loudly visible, and a deployment owner may want an override.

Nothing else lands, which is what stops one broken commit from poisoning every worktree branched
from it, and it holds only that project's lock — a red trunk in one project never blocks another.
This is
the one case where a gate result changes scheduling rather than only recording history, and it is
deliberate — without it, "detected and undone" is a claim the system does not honor.

The manifest schema, the output envelope schema, and the project-side gate SDK are specified in
[base-engineering.md](base-engineering.md).

## Artifact verification — Reactor's half

The project-facing rule is
[invariant 6](base-engineering.md#6-a-steps-completion-is-a-verified-artifact): a step's completion
is a durable artifact that declares how it can be checked. Reactor's half is that **the check
actually runs, and runs somewhere the step does not control.**

1. **Accept nothing unchecked.** A step reporting done submits its artifact; Reactor validates it
   against the declared check *before* recording completion. This is the same discipline as the
   [gate envelope](base-engineering.md#gate-output-envelope) — a subprocess emits a claim, the
   server validates it, and the human-readable part is never what is trusted.
2. **A failed check is a step outcome, not a fault.** The step is simply not done. It returns to
   resolution carrying the failure as context, which is a better input than the one it had the
   first time. Recording it as an error would misroute it to infrastructure-versus-process
   [triage](#infrastructure-failures-and-process-failures-are-different-things), where it does not
   belong: the work *was* evaluated, and the answer was "not yet".
3. **Where the check runs follows what it reads.** A check over item state runs server-side; a
   check over the tree runs on the arena holding it, and where the check *is* a gate run it is
   scheduled, serialized, and budgeted as one. **Cheap and structural is worth designing for** —
   "does this commit's tree contain the change", "is this patch non-empty *or* already in `HEAD`" —
   because a check that costs as much as the step doubles the price of every completion.
4. **Verification is billed to the step's grant.** It is work, not bookkeeping, and an unbilled
   check is exactly the unbudgeted step the [grant ladder](#the-grant-ladder) exists to rule out.

**Single-authorship is enforced here too.** Reactor rejects a write to an artifact the submitting
step does not own, rather than trusting steps to stay in their lanes. Without that, one step's
re-run can invalidate a neighbour's verified state, which destroys correct work and is invisible
until something downstream needs it.

## Engagement — how the system reaches a human

Autonomy by default with deliberate escalation means the system decides for itself and routes a call
to a person only when it judges the call should rise. That routing needs a surface, and human
attention is the scarcest resource the fleet consumes — so the surface is a **scheduler for
attention**, not a notification list. The full specification is
[engagement-feed.md](engagement-feed.md); Reactor's obligations are five.

1. **Everything reaches a human through one surface.** A flow step, a gate, or Reactor itself posts
   an **article** — a self-describing call to attention with its own calls to action. Reactor never
   branches on who posted it, so a component it has never heard of can reach a person after a
   data-level registration rather than a code change.
2. **The feed holds no authority and no history.** An article projects something already durable
   elsewhere — an item annotation, a gate result, a ledger record — so **wiping the feed loses
   attention and nothing else.** This is the [arena rule](#a-host-that-is-merely-off-is-not-a-host-that-is-gone)
   applied to a second store: feed-held state is an optimization, never authority.
3. **Ranking is scheduling, not relevance.** Articles are ordered by **regret per minute of
   attention** — what it costs to leave this undone, divided by the human minutes it takes to
   dispose of it. The largest input is one no emitter can know, so Reactor computes it: what work is
   blocked behind this, from the graph it already owns. The fold falls where the reader's attention
   budget runs out rather than at a fixed score.
4. **A question is an item annotation; the article is its projection.** Questions and answers are
   durable on the item because they steer autonomous work; the article that carries them decays,
   is dismissed, or expires without touching either. A question's *kind* determines both who may
   answer it and how long they have — policy, never the asking step, since a step setting its own
   deadline is choosing how long its supervisor gets.
5. **Taking an action confers nothing.** A card is a shortcut to an operation the reader could have
   invoked directly, performed **as that reader** and checked exactly as if they had. The feed is a
   channel from low-trust agents to high-trust humans with buttons attached, so provenance is
   stamped rather than claimed and an action may only fire from a card that carries everything
   needed to decide.

**An answer is input, not authorization.** It feeds the work; it never feeds the bound. No answer
widens `role ∩ step`, and a human wanting to widen a grant edits companion-repo config rather than
writing a comment an agent will read.

## Cross-project work — change sets and blocking edges

> The project-facing rule is [invariant
> 5](base-engineering.md#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped); this
> is Reactor's half.

Because a step writes exactly one project, a change spanning several decomposes into one item per
project. What Reactor adds is a way to say *these belong together* and *this one waits for that one*
— without either becoming a second place project knowledge lives.

### Change sets group; they are never resolved

A **change set** is an aggregate: N per-project items, plus the edges between them. Nobody resolves
it and it dispatches nothing. It exists so "3 of 5 landed" is answerable, and so a decomposition has
an identity after the step that produced it has finished.

Three shapes arise, and only one of them needs an edge:

| Shape | Example | Edges |
|---|---|---|
| **Independent** | the same lint fix in five projects | none — all resolve in parallel |
| **Producer → consumer** | one project publishes, another bumps its pin | consumer waits on producer |
| **Discovery** | resolving in A, the real fix is in B | created mid-flight by the discovering step |

**Reactor owns the change-set record.** Items are issues in a project; a change set spans projects
and has no project to be an issue in. Putting it in one participant's repo would make that
participant arbitrarily special, so this is the one item-shaped thing Reactor stores outright rather
than mirroring from a code host. The cost is real and worth naming: it is a population that exists
nowhere else, so it must be visible in the admin UI or it is invisible entirely.

**A code host's own cross-repo boards are a projection, never the source.** Reactor may render its
edges onto one for humans to read, one-way. It must not read them back: answering "is this
unblocked?" has to be mechanical on a slow tick, and a board somebody maintains by hand is exactly
the mirrored-project-knowledge failure [no manual gate
registration](base-engineering.md#no-manual-gate-registration) exists to prevent. Such boards are
also owned by one org, which cannot express a dependency on a project in an org you do not control —
the case the artifact edge below handles instead.

### An edge names a target and a condition, never a version

The producer's release version is not knowable when the edge is created, so an edge that hard-codes
one is wrong the moment it is written. The edge names *what it waits on* and *what counts as
satisfaction*; the producing item publishes the fact that satisfies it.

**Two conditions**, and the difference between them is that landing is not releasing:

| Condition | Unblocks when | Use when |
|---|---|---|
| `landed` | the producer item reaches a terminal landed state on trunk | the consumer only needs the change to exist — it vendors by path, or its next rebase picks it up |
| `published` | the producer item records the artifact version it produced | the consumer must bump a pin, and a merge that has not been cut into a release gives it nothing |

**Three target kinds**, because the producer is not always a Reactor item:

- **A Reactor item** — in-deployment, either condition above, and it requires the target project to
  be in the waiting item's read scope.
- **An external issue** — a URL whose state Reactor polls.
- **An artifact version** — "unblock when `promise@≥0.9.0` exists."

The third is the one that crosses boundaries the first two cannot. **A published version is a public
fact requiring no shared permissions, no common organization, and no agreement between the two
projects that they are related** — so a project waiting on a language feature can express exactly
that, while remaining invisible to the project it waits on, and vice versa.

### Blocked is a recorded state, not a stall

- **Cycles are refused at edge creation.** A waiting on B waiting on A is a deadlock that will
  otherwise be found by load. Reject the edge that would close a cycle; if one is somehow reached,
  park it with a stated reason rather than letting the scheduler discover it as silence.
- **Blocked items surface by age; they never expire.** An item blocked for months on an external
  issue is indistinguishable from an abandoned one unless something ages it into view — but
  expiring it would discard correct, completed work. So it rises in the admin UI and stays
  resolvable.
- **Blocking normally releases the arena.** A blocked item is not about to run, and blocking
  happens at a clean step boundary — the discovering step commits its work, files the blocker, and
  reports — so what remains on the arena is warm cache whose value decays to nothing over the
  timescale a block lasts. The [binding](#an-arena-is-leased-to-an-item-not-to-a-step) is therefore
  released outright rather than merely deprioritized, because it is protecting nothing. The
  exception is the rare case where an item becomes blocked while a step is interrupted mid-flight:
  that arena holds unrecoverable state, so the binding is kept and ranks first among victims under
  pressure.

### Relocation is a link, not a closure

Work discovered in one project whose fix belongs in another has to end up in the right queue, and
how the *original* item ends matters as much as where the new one starts.

> **An item whose work belongs elsewhere closes as `moved to <project>#<id>`** — a distinct outcome
> from resolved and from declined, carrying a durable pointer to its successor.

Without that distinction the two collapse, and the collapse is not cosmetic: an item closed for
being in the wrong repo reads later as an item that was refused, so a wanted fix looks like a
rejected one and the successor has no provenance. This is the item-level form of a rule the fleet
already applies to processes — *termination always produces a verdict; nothing terminates into
ambiguity, because an ambiguous outcome is what later becomes a stall.* A closure reason that has
lost its meaning is that ambiguity, arriving months later and misleading whoever reads the history.

The pointer is one-way and cheap: the successor need not know about its origin, which matters
because the two projects may be mutually invisible under
[read scope](base-engineering.md#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped).

### Serialization is per project, not per fleet

The [integration lock](#every-exclusion-is-held-by-a-process-never-by-a-flag) is **per project**. A
single global one would make every project's landings queue behind every other project's for no
reason at all, which is a throughput bug that only appears once there is more than one project — and
the same applies to [trunk-red preemption](#gate-execution--reactors-half): red in one project must
never hold another project's lock. Exclusions that are genuinely about a shared physical resource —
the per-host verify lock, `host:cpu` — stay host-scoped, because that contention is real regardless
of which project caused it.

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
| P14 | **Child-process control beyond spawn/kill/wait** | `Process.kill` sends SIGKILL only; no process groups; no way to signal or wait on a pid this process did not spawn | the runner's [watchdog](#nothing-runs-unwatched) — graceful termination, killing a process *tree*, and cleaning up a previous life's children after a restart |
| P15 | **Argument passing through `promise run`** | passes **no** arguments at all; a trailing token is misread as the source path and `--` is not a separator | every Promise dev tool, and therefore `bin/gate list --json` — the one command BASE requires of a project |

**P15 — tool arguments.** A dev tool that cannot take arguments is not a dev tool, and this sits
directly under the only contract BASE mandates. It is listed as **blocking** rather than worked
around, because the available workarounds are both wrong: shipping compiled shims rebuilds the forge
machinery the Promise tooling model exists to delete, and reshaping the contract into something
argument-free would leak a Promise limitation into a surface that is deliberately language-neutral —
a Rust project satisfies it with `cargo` and has no argument problem at all. See
[promise-forge.md](promise-forge.md), requirement 3.

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

**P14 — child-process control.** `os.Process` already gives spawn with piped stdio, `wait`, `kill`,
and `id`, and that is enough for the *shape* of the watchdog: a goroutine blocked in `wait` sending
the exit code down a `Channel`, selected against a timer goroutine, with the M:N scheduler making
one watcher per child cheap. Four things are missing, each of which the
[process discipline](#nothing-runs-unwatched) above depends on:

- **A graceful signal to a child.** `kill` is SIGKILL only, so there is no term-then-kill ladder —
  an agent gets no chance to flush state or release a lock cleanly. Signal *handling* exists
  (`setup_signal_handling` / `receive_signal`); signal *sending* to a child does not.
- **Process groups / job objects.** A spawn option to put the child in its own group, and a kill
  addressed to the group, so a flow's agent and that agent's compiler die with it. Without this,
  killing on deadline leaves grandchildren running and the arena accumulates orphans.
- **Signalling and reaping a pid this process did not spawn** (POSIX `kill(pid, 0)` for liveness and
  a signal by number). After a runner crash the recorded pids belong to a dead parent; today there
  is no handle to reach them, so the restart cleanup has nothing to call.
- **Process start time for a pid**, which is what makes `(host, pid, start time)` — the identity
  every [lease](#every-exclusion-is-held-by-a-process-never-by-a-flag) is keyed on — checkable
  rather than assumed.

The first two are unconditional. The last two could be avoided by having the governor own child
cleanup instead of the runner, but that pushes item-shaped knowledge into a component whose entire
value is knowing nothing — so they are the better ask.

### Non-blocking, wanted later

| # | Capability | Today | Needed for |
|---|---|---|---|
| P7 | KV / object-store client (`cloud`) | design only | the KV `ConfigStore`/`LedgerStore` backends |
| P8 | `toml` | stub | config ergonomics; JSON is sufficient meanwhile |
| P9 | `schema` | design only | manifest and API payload validation; hand-written validators meanwhile |
| P10 | `--target` cross-compilation | planned (runtime-architecture phase 7e) | collapses the runner/governor/flow release matrix to one build job; a native CI matrix works meanwhile. Matters more for flows, whose matrix multiplies per project |
| P11 | Tool-source-directory discovery + compile caching | see [promise-forge.md](promise-forge.md) | replacing the Go `./make` blueprint with `promise run <tool-dir>` |
| P12 | Addressing a module in a repo **subdirectory** | **landed in head** — `subdir` on `[require.NAME]`; ships with the next release cut | granular modules out of one BASE repo — wire types, gate SDK, and flow library each addressable on their own |
| P13 | Partial clone for remote modules | `git clone --bare`, full history, no `--filter` | any remote dependency pulls the entire repo and its history |

**P12 — subdirectory modules: landed in head**
([promise#30](https://github.com/promise-language/promise/issues/30)), shipping with the next
release cut — so BASE can be laid out for it now, and nothing pinned to a released channel can
consume it until that cut happens.

The subpath lives on the **`[require.NAME]` entry** rather than in the location string, and the
`repo//subdir` spelling other ecosystems use is rejected at parse time — such a URL normalizes to
exactly the identity of `url` plus `subdir`, so the two spellings would silently share a pin, a
resolution slot, and an IR prefix. Module identity becomes `<normalized-url>//<subdir>`, giving each
addressed module its own IR prefix and cache entry, while the bare repo and checkout stay keyed on
`(url, commit)` so several subdir modules in one repo share a single fetch.

**What it buys BASE.** Because the field is on the *named* require form, several named entries may
point at one URL with different subdirs — so one repo can publish N independently addressable
modules. That is what makes the single-BASE-repo decision work: Reactor can depend on the wire types
alone, a flow on the wire types plus the common library, and a project's gate on the gate SDK alone,
without any of them pulling the others into their compilation.

**What it does not solve is P13.** Addressing is not fetching: a consumer that wants only the gate
SDK still clones the whole repo with full history. Shared fetch amortizes that across modules taken
from the same repo, but it does not shrink the first one — so keeping the gate SDK cheap for an
outside project still depends on partial clone landing.

### Already sufficient

`os` (process spawn with piped stdio, env, cwd, signals, exec, kill, wait), `io` (files,
directories, buffered readers/writers, metadata), `json`, `time` (wall clock, monotonic `Instant`,
`Duration`, sleep), `path`, `net` (TCP listener/stream with reactor-based goroutine parking),
`std` (`Mutex`, `Channel`, `Task`, `select`, `` `embed ``, `Builder`, collections). These cover the
repo-backed stores' data handling and the concurrency model outright, and they carry the *structure*
of subprocess supervision — spawn, watch in a goroutine, select against a deadline. Only the sharp
edges of process control are missing, and those are P14.

## Open questions

What is genuinely undecided. Everything else in this document is a statement about the system, not
a proposal awaiting approval.

1. **What the web surface costs.** The [platform requirements](#platform-requirements--requested-of-promise)
   cover the fleet's needs; the admin UI, the feed's ranker, and its sweep are unpriced.
2. **The capability vocabulary will keep growing**, and each addition has to answer the same
   question — what does this let an agent reach that `role ∩ step` could not otherwise describe?

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
- **Never stall, never spin.** Every wait is backed by a live process and a deadline; every attempt
  that costs tokens or machine time must differ from the one it repeats. See
  [Reliability](#reliability--never-stall-never-spin).
- **Everything the runner starts is a separate process with a pid, and nothing runs unwatched.** No
  unbounded waits, liveness read from the OS rather than from output, kill the process group rather
  than the child, and a restart adopts nothing.
- **Locks are leases held by `(host, pid, start time)`, never flags.** An integration lock, a
  per-host verify lock, a worktree, an item claim — all the same primitive, all released
  automatically when the holding process dies. More generally, every piece of persisted global state
  is either **held** by a currently-executing process or **time-bound**; nothing persists just
  because it was once written.
- **Infrastructure failures and process failures are separate classes**, distinguished by whether
  the work was ever evaluated. Infrastructure failures are never charged to the item, are handled
  fleet-wide rather than per item, and resume at the step boundary; process failures are results and
  are never retried unchanged. An unclassified failure is treated as a process failure, because that
  is the mistake that stops.
- **An arena is in exactly one of four states — `leased`, `reserved`, `idle`, `offline` — and
  holds at most one lease at a time.** A lease names a *work unit*, not necessarily an item: a
  scheduled gate run and a job Reactor runs for itself hold one too, transiently, so a machine is
  never occupied by something the scheduler cannot see. An item's lease is sticky and a transient
  one is never a victim of capacity pressure; `reserved` keeps a person's own machine out of the
  pool, on its own expiry because a human cannot be named as a pid; only `idle` accepts a new
  lease, which makes monitor cadence best-effort with misses recorded as contended. A host is not
  an arena until **adopted** — a separate, longer-lived trust decision that registration does not
  confer and a write-off does not revoke. Adoption is of the *host*; an arena is born in the state
  its host's adoption record names, defaulting closed, and a lapsed reservation returns it there
  rather than to the pool.
- **An absent arena is held on a long clock, then written off.** Work leases expire in minutes so
  the fleet never waits on a machine that went away, but the arena *record* is retained across a
  temporary absence (default 24h) before the arena is declared lost. Anything left on a lost arena
  is gone, a returning host is a new arena, and the write-off is recorded in the ledger.
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
- **Reactor distributes flow binaries but builds none of them.** One binary per project, keyed by
  `(project, os, arch)`, built by the companion repo's CI and published as a release that Reactor
  ingests and serves. The runner resolves the version **per step, not per item**, so a
  mid-resolution fix is picked up by an item's remaining steps.
- **Reactor is three deployables, not one** — `bin/reactor` (cloud), `bin/runner` and
  `bin/governor` (in the workspace, on other machines or containers).
- **The server never reaches into a host.** Runners always initiate outbound and long-poll. Cloud
  arena provisioning is the sole exception, since there is no host to run an arena-host runner on.
- **A governor supervises runners; it is not the machine.** It may be system-wide (launching
  runners as several OS users) or per user (needing no privilege), and it holds an exclusive lease
  keyed `(machine, os user)` so two governors can never supervise overlapping sets — locally
  enforced before the server is reached, and server-side so a cloned image cannot present an
  adopted machine's identity twice. `host:` exclusions key off the machine, and a sandbox inherits
  its parent's, so splitting supervision never doubles a box's apparent capacity. The governor
  knows nothing about items, gates, or arenas — so sandboxes are supervised instead by the
  arena-host runner that created them, which can do everything a governor can *and* destroy and
  recreate them. **A sandbox never carries a governor**, however long it lives. Where a governor is
  needed follows from the outbound-only invariant: a cloud VM has nothing local above it, a local
  sandbox does.
- **Keep cloud arenas** — mostly implemented, and the practical way to run cross-platform gates.
- **The public repos are promise, flow, forge, reactor, and base**; cross-repo deps are versioned
  dependencies, not submodules.
- **The BASE layer is one repo**, publishing several independently addressable modules as
  subdirectory modules. Reactor takes the wire types alone, a flow takes wire types plus the common
  library, a project's gate takes the gate SDK alone.
- **Wire compatibility is explicit, not assumed.** A `schema_version` on the wire, unknown majors
  refused, additive-only evolution within a major, and persisted step state readable across a flow
  version change. A shared module removes hand-synchronization, not skew.
- **A flow describes itself; the system constrains it separately.** Operational facts come from the
  flow's `describe`; roles, grants, capabilities, and read scope are read from companion-repo config
  Reactor loads independently. See
  [What a flow declares](#what-a-flow-declares-and-what-is-declared-about-it).
- **Which pocket pays is the deployment's business, not the step's.** Subscriptions are ambient to
  an arena, API keys are injected by Reactor from admin config, and no account appears in a step
  declaration.
- **A step's completion is a verified artifact** — durable, checked by the system before completion
  is accepted, and writable only by the step that owns it. One author and one independent check is
  what lets the artifact set be the only completion record.
- **Budgets are grants that escalate, not ceilings that stop.** Extension is decided on evidence of
  progress, is itself a capability with a per-role limit, and stops at a hard ceiling only a human
  may raise. A spend counter cannot tell a hard item from a stuck one.
- **Relocation is a link, not a closure.** An item whose work belongs in another project closes as
  *moved to `<project>#<id>`*, distinct from resolved and from declined.
- **Steps dispatch themselves; no per-item plan is stored.** The resolver scans declared steps,
  `check` answers cheaply and without a model credential, `run` is invoked for the first
  unsatisfied one, and the scan concludes with `complete` or `handoff`. A stored plan cannot
  survive per-step flow version resolution, which the design already promises. *(Proposed — see
  [Step resolution](base-engineering.md#step-resolution--steps-dispatch-themselves).)*
- Build tooling is the **forge blueprint** (`./make`, `bin/verify`, ratcheted baselines, guard).
