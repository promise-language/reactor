# BASE Engineering — the project-facing layer

> **This document defines what a project adopting BASE must provide, and what BASE provides to it**
> — the six invariants, the gate contract, step resolution, and what lives in which repo. Some of it
> is owned and built by the project (gates); some is project-specific but deliberately versioned
> outside the project's own source (flows).
>
> **It assumes** the methodology in the [white paper](../WHITEPAPER.md).
> **Depending on it:** [design.md](design.md), which is Reactor's half of every contract here — how
> it discovers, schedules, distributes, and executes.
>
> What is undecided is marked inline; everything else here is a statement about the system.

## Invariants

> **These are the load-bearing six.** Everything else in this document
> — the manifest, the gate taxonomy, the tool layout — exists to serve them. A system that satisfies
> them by other means is fine; a system that does not satisfy them is not BASE.

### 1. Origin is always green, on every platform

**Nothing reaches origin that would fail the project's verify set, and a fresh clone verifies green
on every supported platform.** Not "usually", and not "green on the platform that happened to push
it".

The reason this is an invariant rather than a quality goal is blast radius. A broken commit in origin
does not cost one actor a rerun — it poisons every worktree branched from it, so with N resolutions
in flight the fleet loses N, and every subsequent verify fails for reasons unrelated to the work
being done. Recovery serializes behind one fix while everything else idles.

A single pushing host can only prove its own platform, so enforcement splits by what is provable
where:

- **Same-platform verify is preventive.** It blocks `push:origin`, server-side, with no bypass —
  see [Preconditions are enforced at the boundary](#preconditions-are-enforced-at-the-boundary).
- **Cross-platform verify is detective, with mandatory preemption.** The matrix cannot gate a push
  from one host at acceptable cost. That is permitted by the [materiality
  test](design.md#where-it-is-enforced) — "prevented, *or* detected and undone" — but only if the
  undoing actually happens. So a failing monitor **files a repair item, and that item holds the
  project's integration lock** until green, dispatched ahead of the queue. Nothing else lands until
  it is, which is what stops the poisoning cascade — and giving the lock a *holder* rather than
  making redness a flag is what keeps it inside
  [the lease rule](design.md#every-exclusion-is-held-by-a-process-never-by-a-flag), releases it
  automatically if the repair dies, and leaves the repair itself able to integrate because it holds
  the lock rather than being excluded by it.

**Prefer to make it preventive anyway, by running the matrix pre-merge on the PR.** There, latency
hides — PRs verify in parallel while integration serializes — whereas fanning the matrix out under
the integration lock caps trunk throughput at `1 / matrix-latency`, which for a 30-minute matrix is
roughly 48 landings a day. Detection-plus-preemption is then the backstop for what still slips
through, not the primary mechanism.

**"Clone and verify" means from a bare clone.** Verify must depend on nothing uncommitted and on no
warm local state for *correctness* — only for speed. That is what `preflight` is for, and a CI job
that clones fresh and runs verify is the cheapest way to keep it true.

#### What "every platform" means

The word carries three different meanings, and the invariant is only well-defined once they are
separated:

| | What it is | Declared as |
|---|---|---|
| **Target** | what the product is built *for* — the thing green must hold for | project-level `targets` |
| **Host** | where a gate process runs | `host_os` / `host_arch` on the gate |
| **Runnable here** | whether a given host can execute the gate for a given target | not declared — *derived* from the two above |

> **"Origin is green on every platform" means: for every declared target, the gates that speak for
> that target are green.**

Host and target coincide often enough that conflating them mostly works, and then stops. A
`wasm32` target has no host of its own — it is built anywhere and run in a runtime. A Windows target
can be *built* from another host wherever cross-compilation exists, but its tests generally cannot
be *run* there. So a gate declares where it can run and, when it differs, what it speaks for;
whether some host can cover some target is then an ordinary eligibility question rather than a new
concept.

**A single-target project needs none of the machinery below**, and this is what makes that
determination crisp: one entry in `targets`, so the matrix is a matrix of one.

#### Choosing preventive per change

Running the full matrix on every item is safe and caps throughput; running it on none is fast until
a platform-divergent change lands. Neither is right, so **which mode a given item gets is a decision
the item carries**, and it has three parts: a cheap floor that always runs, a predictor that
escalates, and an automatic escalation once trunk is already red.

**Build every target, where that is affordable.** Not the test suite — just the build. Platform-
divergent code characteristically fails to *compile* for another target long before it fails a test
there, so this tier catches most of the damage for a fraction of a full verify.

How cheap it actually is depends on the toolchain, and the honest answer today is *not as cheap as
it sounds*. With cross-compilation it collapses to one host building every target, which is close to
free. Without it — Promise cannot cross-compile yet, see
[P10](design.md#platform-requirements--requested-of-promise) — building every target means
dispatching to an arena per target, so the *coordination* costs the same as the full matrix and only
the occupancy is smaller: minutes of a machine instead of half an hour. Still worth having, but a
project whose toolchain cannot cross-compile should size this tier against its arena capacity rather
than assume it is free.

**A predictor escalates an item to the full matrix**, and the signal is narrower than it first
appears. The dangerous change is not one that touches every platform — it is one that touches
**a single variant of something that has several**, because the author sees and exercises only the
variant they touched. That inverts the obvious heuristic, and it is what makes the failure mode so
reliable: nothing in the change looks cross-platform.

Signals worth deriving from the diff, all structural rather than stylistic:

- it edits one platform variant of a module that has others
- it changes an interface without moving every implementation of it
- it touches a path whose gate history already records platform-specific failures — the ledger keeps
  per-platform gate results, so this is a query rather than a guess

**Decided at `plan`, escalated freely later, de-escalated only as a recorded exception.** Plan is
where someone is already reasoning about the shape of the work, so it is the cheapest moment to ask
the question; the actual diff can escalate what the plan underestimated. The asymmetry is
deliberate — forcing the matrix on costs latency on one item, forcing it off is how a cascade
starts.

**Tune for precision, not recall**, because the two errors cost wildly different amounts. A false
positive is one matrix run of latency. A false negative is a red trunk, a repair cycle, and every
other item stalled behind the integration lock for the duration. Prefer few, structural,
high-confidence signals and accept that some divergent changes slip through — the next rule bounds
what that costs.

**Once trunk is red, every landing clears the full matrix, automatically and with no flag.** This is
damage control rather than prevention, but it is free: the [repair item already holds the
integration lock](design.md#gate-execution--reactors-half), so nothing else can land regardless and
the matrix costs no throughput that was available anyway. Without it the repair loop inherits the
defect it is repairing — a one-platform fix for a one-platform break, verified on one platform,
which is how a single divergent change turns into several rounds of breaking one platform to fix
another.

**All of this is optional per project.** A single-platform project needs none of it, and should not
pay for the machinery.

### 2. A step changes the tree only by committing, and leaves it clean

**A flow modifies the worktree through exactly one path: it commits.** At the step boundary the tree
is clean — no stray files, no staged-but-uncommitted work, no build output.

This is not hygiene. It is what makes every other enforcement layer well-defined: the post-hoc diff
audit has a precise subject, the next step inherits no contamination, and "what did this step do"
has exactly one answer. The same postcondition applies at gate granularity, and Reactor checks it
rather than leaving the flow to honor it.

**What counts as clean is the tree's own business.** The check is that `git status` reports nothing —
which permits build output in a directory the project already ignores, and permits nothing else. The
ignore rules *are* the declaration of expected residue: versioned with the tree, reviewable in a
diff, and the same one git, the gate runner, and the step-boundary check all read.

#### One delivery path per step

A step that can hand back its work three ways — files left in the worktree, changes staged, or a
commit — has three success paths and rather more than three failure paths, and the combinations
multiply the moment a step runs twice. Left in the tree or staged, the work also violates this
invariant outright, so the extra routes were never legitimate.

> **Each step delivers its result exactly one way.**

For `implement`, that way is **a single commit on the item's branch, amended on every subsequent
run.** Amending rather than appending is what keeps the artifact one shape no matter how many
attempts it took: "which of these commits is the implementation" stops being a question anyone has
to answer. Progress remains visible because the commit *hash* changes, and the ledger keeps the
prior hashes even though the branch tip no longer points at them.

Completion is then a predicate anyone can check, which is what
[invariant 6](#6-a-steps-completion-is-a-verified-artifact) requires of it:

> **`implement` is done when the item's branch carries one new commit, the worktree is clean, and
> the push-blocking gate set is green.**

**One consequence to make explicit:** amend-based delivery requires a force-push to the claim's
branch. That is safe there — nothing downstream builds on a work branch, which is why
[invariant 1](#1-origin-is-always-green-on-every-platform) does not gate `push:branch` — but it must
be impossible on trunk. So `push:branch` permits a non-fast-forward push and `push:origin` never
does, and the two must not be collapsed into one grant. Which branch that is, is not a choice the
step makes — see below.

#### Branches are mechanical, and there is exactly one per claim

Branches created by judgment accumulate. Work gets done on one, nobody carries it further, and the
repo fills with refs no process owns — which is why the current setup blocks branching outright and
does everything on trunk. The fix is not to keep branching away from agents by convention; it is to
take the decision away from anything that could decide differently.

> **The branch name is a pure function of the claim. Nothing smart chooses it, and nothing smart
> creates it.**

A derived name buys three properties at once. Creation is **idempotent** — create-or-reuse, so a
rerun cannot fork a second branch. The mapping is **total**, so a branch always has an owning claim
and an owning claim always has exactly one branch. And because it is total, **orphans are
enumerable**: any ref whose claim is gone is garbage by set difference, rather than something you
hope did not accumulate.

**Keyed on the claim, not the issue.** One issue can legitimately carry two resolutions — two
contributors, two PRs, as [Single-issue work](#single-issue-work-first-class-prs) describes — so
keying on the item alone would either collide those two or forbid a case the design already
supports. The claim is already first-class, coordinated by assignee plus the lease ledger, so it is
the natural key. **A PR item does not get its own branch**; it names the branch its originating
claim created, which is what lets a review flow run against the PR item without creating anything.

**The lifecycle needs an owner, or gating creation just moves the problem.** A branch is retired
when its claim reaches a terminal state — merged, declined, abandoned, or
[relocated](design.md#relocation-is-a-link-not-a-closure) — and the sweep that finds strays is the
set difference above. An item that is
[paused by a hold](design.md#the-states-and-what-they-belong-to) is not terminal and keeps its
branch, because paused work is waiting, not abandoned.

**What enforces it is the push, not the local repo.** A step with a shell can always run `git
branch` locally, and preventing that is not worth attempting. It does not matter: a local ref that
cannot be pushed dies with the
[ephemeral arena](#4-an-items-work-binds-to-an-arena-and-carries-its-state-forward). So
`push:branch` is scoped to *the claim's own branch* — the same `:own` shape as `artifact
write:own` — and creation proper is `branch.create`, a capability the mechanical path holds and no
agent-driven step does.

#### Invariants and properties are enforced differently

Some things must never be in the tree at all — a committed binary, a secret, a suppression tag like
`ignore_leaks`. Others are transiently false by nature — tests pass, the build succeeds, formatting
is complete. Both are enforced at push. They differ in how *early* they can also be enforced, and
the test is one line:

> **Refuse early iff the violation is never legitimate in any intermediate state.**

| | **Invariant** | **Property** |
|---|---|---|
| Evaluable on | a single write or diff | the whole tree |
| Legitimate mid-work? | never | routinely — that is what mid-work means |
| Examples | committed binary, secret, forbidden import, `ignore_leaks` | tests pass, build succeeds, formatted |
| Enforced at | write · add · commit · push — all of them | reported earlier, blocking at push |

Blocking a *commit* on a property would be wrong: an agent that cannot commit cannot record a floor
to fall back to, cannot produce a bisectable history of its own attempt, and cannot leave the tree
clean at a step boundary as invariant 2 requires. Blocking a commit on an invariant is right, and
refusing the write that introduced it is better still.

**Every such rule is declared once and enforced at many points.** Four hand-maintained copies of "no
binaries" drift, the write-time check passes, the push rejects anyway, and the agent's early
feedback becomes untrustworthy — at which point it stops being worth running and everything is
discovered at the boundary again. "The sooner the agent knows, the cheaper the fix" holds only if
the early answer is *exactly* the late verdict, never an approximation of it.

### 3. Serialization is declared, and waiting for it is not work

Gates are time-bound, and some must be serialized — to avoid merge conflicts, or simply to not swamp
a machine's resources. Those two facts interact badly unless handled explicitly:

> **A step declares what serializes it, and time spent waiting on a declared exclusion does not
> count against the step's deadline.**

Charging queue time to the work deadline makes a timeout a function of fleet load, so steps begin
failing under contention — exactly when the system is busiest and a false failure costs most. The
deadline must measure work.

Four things follow, none optional:

- **A name is `<scope>:<leaf>`.** The scope — `project`, `host`, `arena`, `global` — is understood
  by Reactor and the leaf is opaque to it, so a project can invent `project:migration` without a
  change to the shared layer, while Reactor still knows that `project:integration` in two projects
  is two locks and `host:cpu` on one box is one.
- **The declaration is static.** A step names its exclusions ahead of time, so Reactor can acquire
  them in a canonical order. Locks discovered at acquisition time cannot be ordered, and unordered
  acquisition of more than one lock is a deadlock waiting for load to find it.
- **The set is transitive.** A step that runs a gate inherits that gate's exclusions — the per-host
  verify lock, an exclusive worktree. The effective set is the step's own union everything it
  invokes, computed rather than hand-listed, or it drifts like any other duplicated declaration.
- **Waiting keeps its own deadline.** Excluding queue time from the work clock must not reintroduce
  an unbounded wait, which would trade a false failure for a
  [stall](design.md#reliability--never-stall-never-spin). Two deadlines: the work deadline the step
  declares, and a queue deadline bounding how long it may wait to start. Exceeding the queue
  deadline does not fail the step — it returns to the queue, recorded as contended, which is a
  capacity signal rather than a defect.

### 4. An item's work binds to an arena and carries its state forward

Invariant 2 describes the step *boundary* — what the tree looks like when a step ends. It says
nothing about the much larger body of state a resolution accumulates and that never belongs in a
commit: the agent's on-disk session and notes, scratch files, partial downloads, a warm compile
cache, the materialized worktree itself. That state is what makes step N+1 cheaper than starting
over, and what makes an interrupted step resumable at all.

**Capturing it is not the answer, and it is worth saying why.** The tempting design is to serialize
progress somewhere durable and restore it onto a fresh arena. It fails three ways:

- **What matters cannot be enumerated.** The relevant state is whatever the agent and its toolchain
  happened to write. That is not knowable in advance and changes with every tool.
- **Much of it belongs nowhere durable.** Caches and build output are large and machine-shaped, and
  committing them is exactly what invariant 2's forbidden-content rules exist to stop.
- **The capture would not run.** Capture is cooperative code, and the terminations that matter are
  the non-cooperative ones — a hard kill, a crashed runner, a host that went offline. Termination is
  often *caused by* the arena being unreachable, in which case nothing on it executes at all. A
  recovery path that depends on cleanup having run has no answer for precisely the cases it exists
  to handle, which is the same rule the fleet already applies to leases: [no correctness property
  may depend on cleanup code having run](design.md#nothing-runs-unwatched).

So the state does not move. The work goes to it.

> **An arena is leased to an *item*, not to a step.** The lease is taken at first dispatch and held
> across every subsequent step of that resolution, so each step inherits what the previous ones
> accumulated. Nothing is identified, serialized, or restored, because nothing moved.

What this buys is not merely convenience — it removes the requirement to *know* what needed
preserving, which is the requirement no capture design can meet. **Interruption is then not a
special case but the degenerate one:** a step that died mid-flight resumes on the arena that still
holds its dirty tree, by the same rule that lets `implement` inherit `plan`'s notes.

- **`item → arena` is first-class persisted state.** Dispatching a step elsewhere does not merely
  misroute it, it silently discards the accumulated state — which is worse than refusing, and is why
  the binding is recorded rather than inferred.
- **Two lease clocks, already present** — distinct from invariant 3's two *deadlines*. The short
  work lease dies with the step's process, as it should — [a runner restart adopts no
  processes](design.md#nothing-runs-unwatched). The arena lease is what holds the disk state, and
  for an item it is [sticky](design.md#an-arena-is-in-exactly-one-of-four-states): it stays with the
  item across steps rather than returning to the pool at each process exit, which is what separates
  it from the transient lease a gate run takes.
- **The binding is released by demand, not by a clock.** An idle arena costs nothing while the pool
  has spare capacity, and breaking a binding always costs something — the transient state is gone.
  Those are not symmetric, so a healthy binding has no expiry: it is reclaimed when capacity is
  genuinely short, and otherwise left alone however long the item sits. The one clock that does
  exist covers a different situation — an arena that has become *unreachable* while its item wants
  to run, where waiting indefinitely would be a stall rather than a saving.

#### A step may declare that it does not want the inheritance

Carrying state forward is the default, not an obligation, and two different relaxations are worth
separating because they cost different things:

| Declaration | Effect | Why a step asks for it |
|---|---|---|
| **Fresh session** | the agent is launched with no inherited notes or context; the tree and caches are untouched | the step's judgment must not be contaminated by the reasoning that produced the work |
| **Arena-independent** | may be dispatched to any eligible arena, rather than waiting for the bound one | the step needs nothing but the tree and the item, so binding it wastes capacity |

They are orthogonal: the first constrains what the agent *knows*, the second what the scheduler may
*do*. `inspect` declares both — its judgment must be independent, and since it inherits nothing
there is no reason to hold it to one machine. The first is the one that matters. **An inspection
that inherits the implementer's session is not an independent check — it is the same reasoning
grading its own output**, which is precisely the property "untrusted work is
[bracketed by trusted gates](#single-issue-work-first-class-prs)" depends on. So this is an
integrity declaration first and a scheduling hint second.

**Shared transient state is an information channel between steps, and it crosses trust boundaries.**
Step grants isolate what a step may *do*; a shared arena creates a path for what it *knows*. Notes
left by a step run under a low-trust role are read by whatever runs next, so a review or security
step inheriting them is taking input from the thing it is reviewing. **Where two consecutive steps
differ in trust level, a fresh session is mandatory rather than optional** — and like the rest of
the step's grants, the declaration lives in the companion repo, out of reach of the agents it
constrains.

**Commits are the floor, not the mechanism.** If the arena vanishes, the transient state goes with
it and the item restarts its current step from the last commit. That is the accepted loss: rarer
than routine interruption, and far cheaper than a capture mechanism that cannot be relied on in the
cases that matter. The two paths differ in kind — **a resume with its arena intact is not a
repeat**, because it starts with the partial tree and the agent's own notes; a from-scratch restart
after arena loss is, and is subject to the ordinary
[loop-detection](design.md#reliability--never-stall-never-spin) rule.

**What survives is what was on disk.** A dead agent's in-memory context is gone regardless; what
carries forward is the tree and whatever the agent and its tools wrote down. That is an argument for
agents that externalize their state as they work, and it is not a property the fleet can enforce for
them.

### 5. A change writes to one project, and reads only what it was scoped

Reactor orchestrates many projects, and work sometimes spans them. Both halves of that need
bounding, and they bound differently:

> **A step writes the tree of exactly one project — the one its item belongs to — and reads only
> the projects its item's scope names.**

**Write, because the alternative breaks three things at once.** A step holding push credentials for
N repos has N times the blast radius, which is the one quantity the authority model exists to keep
small. Pushes cannot be made atomic across repos, so a multi-repo landing has a window in which some
trees are updated and others are not — [invariant 1](#1-origin-is-always-green-on-every-platform)
violated by construction rather than by accident. And an item is an issue in a project; a change
that belongs to several has no home to be an issue in.

**Read is not automatically broad either.** Two unrelated projects can share one Reactor — two
people orchestrating work that has nothing to do with each other — and neither should be able to
read the other's tree merely because they share a deployment. So the default read scope of an item
is **its own project and nothing else**, and anything wider is granted:

> **Effective read scope = deployment tenancy ∩ the item's declared need.**

That is the same composition as [role ∩ step](design.md#authority-roles-steps-and-capabilities),
for the same reason: the deployment owner draws the tenancy boundary, the project declares which
neighbours it actually needs to see inside that boundary, and neither may widen the other. Declaring
the need is least privilege — "everything in my tenant" is not a scope.

**The project declares a default; an item type may narrow it, never widen it.** Item types are
already declared in the companion repo, so per-type scoping costs almost nothing and keeps a
docs-only item from seeing the neighbours a release item legitimately reads. Per-*step* scoping is
ruled out by [invariant 4](#4-an-items-work-binds-to-an-arena-and-carries-its-state-forward): an
arena is bound to an item for its whole resolution, so the set of repos cloned into it must be fixed
before first dispatch and stay stable. Item type is known at creation; the step is not.

**Enforcement is materialization.** The arena clones only what the scope names, read-only. A tree
that was never materialized cannot be read, which puts this in the same class as credential scoping
rather than in the class of rules an agent is asked to respect.

#### Coupling goes through versions, so ordering is enough

The objection to writing one project at a time is the atomic case: a contract change whose halves
must land together. That case should not exist here, and the reason it does not is already in the
design — **projects consume each other by pinned version, never by floating trunk.** A companion
repo pins the flow common library through ordinary module resolution; flows are versioned artifacts
resolved per step. So a producer landing never breaks a consumer. The consumer breaks only when it
chooses to bump its pin, which is its own work item, in its own tree, gated by its own verify.

**The version pin converts an atomicity requirement into an ordering requirement**, and ordering is
easy. Which gives a rule worth stating, because violating it is what makes cross-repo edits feel
necessary:

> **A repo boundary must be drawn where a version boundary can exist.** If two things must change
> together with nothing versioned between them, they belong in one repo. "This change spans repos
> atomically" is a report that the split is in the wrong place, not a request for a feature.

**Nor a request for submodules**, which are [not supported](design.md#the-identity-authority-contract)
and are the usual shape the request takes. A submodule puts a second repository's history inside a
tree that every mechanism here assumes is one — so a change would write two projects, gates would
measure a commit nothing gated, and a claim would need two branches with nothing making them land
together. Vendored content is fine: bytes in the tree are the project's whatever their origin.

#### Discovery files, it does not fix

The common case is not planned at all: a step resolving an item in one project finds the real fix
belongs in another. It must not edit that tree, and it must not stop and wait for a human. It
**files an item in the other project and blocks its own on it.**

So the cross-project grant is `item.create` in a named project — not tree write, not push. **An
agent's reach across a boundary is the ability to ask.** Filing requires that project to be in the
item's read scope, so visibility governs this too: an item cannot file into a project it cannot see.

`blocked on <item>` is then a recorded state with an owner and a resolution path, which is better
than a hold that needs a human, because the blocker is itself an item the fleet can resolve
unattended.
The stall becomes throughput. Reactor's half — change sets, the kinds of blocking edge, and what
happens to the arena — is in
[design.md](design.md#cross-project-work--change-sets-and-blocking-edges).

### 6. A step's completion is a verified artifact

Invariant 2 governs what a step leaves in the *tree*. This governs what it leaves on the *item*, and
it is the difference between a completion record that can be trusted and one that merely exists.

> **Every completed step leaves one durable artifact. The artifact declares how it can be checked,
> the system checks it before accepting the step as done, and no step may write an artifact other
> than its own.**

Three clauses, and each closes a distinct failure:

- **Durable**, or there is no record that survives an interruption, and completion has to be
  re-derived by inspecting side effects.
- **Verified by the system**, or a step is the sole and unchallengeable witness to its own
  completion. That is not hypothetical: a step that records "I pushed commit X" without having
  committed produces a *finalized* item whose work is gone — the worst available outcome, and the
  hardest to notice, because every downstream reader sees a plausible record.
- **Single-author**, or one step's re-run can flip a neighbour's verified artifact back to
  incomplete, destroying correct and already-paid-for state.

**Together they give each artifact one author and one independent check**, which is what makes the
artifact set trustworthy as the *only* completion record. Without both, verification and
single-authorship each fail in the other's absence: a verifiable artifact a neighbour may overwrite
is not trustworthy, and an unforgeable artifact nobody checks is just a claim.

#### Who declares the check, and who runs it

The split is the one already drawn for [everything a flow
says](design.md#what-a-flow-declares-and-what-is-declared-about-it). **The step declares how its
artifact is checked** — that is an operational fact, best known by the code that produces it, and it
travels with that code. **The system runs the check**, because a step trusted to verify itself is a
flow limiting itself.

A claimed completion that fails its check is **not an error**. The step is not done, and it is
returned to resolution *with the failure as its context* — which is precisely the "why am I running
again" input a step needs to do better on the second pass than the first.

#### A step may carry work forward without claiming completion

The artifact is a *completion* record, so a step that has done real work and is not done has nowhere
to put it. That is not a theoretical gap. A `plan` step that drafts half a plan and then needs a
human decision cannot write the artifact — that would claim completion — and
[cannot touch the tree at all](design.md#why-the-step-grant-matters-even-for-a-fully-trusted-actor),
which is the whole point of its grant. Its work is discarded, and the run that resumes after the
answer arrives **redoes it** — the repetition [never spin](design.md#reliability--never-stall-never-spin)
exists to forbid.

> **A step may write one durable, unverified, step-private **checkpoint**: work carried forward
> without a claim of completion.**

- **A checkpoint is never a completion.** Nothing verifies it and nothing reads it as evidence of
  doneness, so the artifact set remains the only completion record and the invariant above is
  untouched.
- **One per `(item, step)`, replaced wholesale, retired when the step completes.** The population is
  bounded by steps in flight, not by attempts.
- **Private to its own step.** Other steps read the artifact, never the checkpoint — otherwise it
  becomes an unverified side channel between steps, which is exactly what single-authorship exists
  to prevent. A step [declaring a fresh session](#a-step-may-declare-that-it-does-not-want-the-inheritance)
  discards its checkpoint too, for the same reason it discards the agent's context.
- **Blocking requires a checkpoint, or an explicit statement that there is nothing to carry.**
  Blocking with work done and nothing written costs exactly as much as spinning.

It is not a store, a lease, or a new layer — it is artifact-shaped minus the completion claim, so it
lives where artifacts live and needs no capability the step does not already hold.

**Three rules elsewhere quietly assumed it existed.** *Blocking releases the arena because the
discovering step commits its work* — a step that may not write the tree cannot.
*Wind-down means reach a verifiable artifact or leave the tree in a state the next attempt can start
from* — same gap, same steps. And *progress is a new verified artifact* would classify every
legitimate block as a loop. The checkpoint is what makes all three true for steps whose output is an
item artifact rather than a commit.

**It also gives loop detection its discriminator.** A blocked run whose checkpoint did not advance
did nothing the previous attempt had not, and *is* the loop case. One whose checkpoint advanced is a
resume, and [a resume is not a retry](design.md#every-attempt-must-make-progress).

#### Verification has a cost, and it needs its own budget

Some checks are cheap and structural: does this commit's tree actually contain the change, is this
patch non-empty *or* already present in `HEAD`. Others are indistinguishable from running a gate —
"the tests pass" is verified by running the tests.

Expensive verification can be optimized but **cannot always be removed**, so it is budgeted as work
in its own right rather than treated as free bookkeeping. A check billed to nobody is an unbudgeted
step, which is the thing the [grant ladder](design.md#every-attempt-must-make-progress) exists to
prevent. Where the check is a gate run, it is scheduled and serialized like any other gate.

## Two layers, often confused

Keeping these apart is the point of this doc:

| | **The BASE layer** (this doc) | **A project's BASE setup** |
|---|---|---|
| What | Reusable, domain-agnostic machinery: the flow common library, the gate SDK, the manifest and envelope contracts, ratcheting baselines, the dev-tooling conventions | One project's *concrete* step composition, item types, prompts, gates, metrics, thresholds, and schedules |
| Owned by | the BASE layer itself — shared across every adopting project | the project |
| Example | step execution, push leases, wire types, "a gate declares metrics with a direction and a mode" | "`promise-tests` emits `test_failures`, enforced, cap 0"; the `implement` prompt template |
| Lives in | a shared repo (see below) | split — gates in the project repo, everything else outside it (today `workspace/projects/<project>/`); see [What lives where](#what-lives-where) |

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
| Dev-tooling conventions | **BASE layer repo** | see [dev-tooling.md](dev-tooling.md) |
| **Per-project flow definitions** — step composition, item types, prompts | **a companion BASE repo, one per project** | project-specific, but must not live in the project tree |
| **Per-project authority config** — roles, step grants, schedules | **the same companion repo** | must be unreachable by the agents it constrains |
| Gate implementations + baselines | **the project repo** | a gate measures the tree, so it comes from the tree |
| **Agent bounds** — the guard hook, tool allowlists, the MCP mount list | **the companion repo**, applied by the arena | authority; must be unreachable by the agents it constrains |
| **Capability-granting MCP servers** — shell, network, database | **the arena image** | capability comes from the environment, never from the tree |
| **Tree-knowledge MCP servers** — compiler introspection, symbol lookup | **the project repo**, read-only | describes the tree, so it comes from the tree |
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

Costs, stated plainly: two repos per project, and cross-repo version pinning between a companion
repo and the flow common library. The pinning is ordinary Promise remote-module resolution, and each
companion repo carries `promise.toml` at its root — so this layout needs no new language feature
([P12](design.md#platform-requirements--requested-of-promise) becomes unnecessary for flows).

### The `.base` directory — how a project names its setup

A project and its companion repo have to find each other, and the obvious place to configure that
is Reactor. That is the wrong place, for the reason
[gate discovery](#no-manual-gate-registration) already gives: it makes a maintainer mirror project
knowledge into the server, by hand, once per project. Having argued that for gates, leaving the
project→companion mapping in server config would be the same mistake with a different noun.

So the project carries it:

> **`.base/` is a directory in the project repo holding a config file that names the project's BASE
> setup** — which companion repo defines its process, and which Reactor deployment orchestrates it.

**It is the second and last thing BASE mandates of a project**, alongside the gate-listing command.
A directory rather than a bare file because more will want to live beside it, and because it matches
how `.github/` and `.githooks/` already read.

#### Declare, then authorize

The pointer is not authority, but it *selects* authority — whoever controls `.base/` controls which
companion repo's roles and grants apply. Left alone, an `implement` step could repoint it at a more
permissive companion and widen itself, which is the self-authorizing bound this whole layout exists
to prevent.

The fix is the composition this design uses everywhere else:

> **The project declares its companion; the deployment must have authorized that pairing.** Reactor
> honors the pointer only when it matches a registration it already holds. Neither side may widen
> the other.

Reactor's half — how a repo becomes a project, what an unadopted one may do, and what happens when
the two sides disagree — is [A repo is not a project until it is
adopted](design.md#a-repo-is-not-a-project-until-it-is-adopted). Two consequences land here: `.base/`
is a **denied path in every tree-write grant**, so no step can edit it; and because it is a claim
checked against a fact, it works as a **tripwire** — an attempt to repoint a project at a more
permissive companion surfaces immediately rather than taking effect.

Same shape as [role ∩ step](design.md#authority-roles-steps-and-capabilities), as manifest ∩
deployment overrides, as tenancy ∩ declared need. A rogue edit then fails closed — Reactor never
authorized that pairing — and `.base/` belongs on the
[protected-path deny-list](design.md#the-capability-vocabulary), so no step can write it at all and
any diff touching it is loud. That makes `.base/` a discovery aid and a cross-check rather than a
grant.

#### Two rules about what goes in it

**It names identity, never a version.** The flow version resolves
[per step](#the-principle) precisely so an async fix reaches work already in flight. A `.base/` that
pinned the companion's commit would fight that directly and reintroduce the contention the
out-of-tree split exists to remove.

**Pointers, never policy.** The hazard of a directory is accretion: someone adds a grant, then a
role, and it quietly becomes the in-tree authority store that
[the companion-repo argument](#why-per-project-base-definitions-get-their-own-repo) spends three
points avoiding. Anything that *bounds* an agent belongs in the companion repo. If a
thing in `.base/` would change what an agent may do, it is in the wrong file.

#### Why a file rather than a field in the gate manifest

The manifest already carries `project` and could carry the companion too, keeping the mandated
surface at exactly one thing. But it is *emitted by a command*, and running that command needs the
toolchain built. **Bootstrapping is circular**: you may need to know the BASE setup before you can
produce the manifest that would have told you. A static file readable from a bare clone breaks the
circle; a command cannot.

### Bounds are authority, not tooling

The same reasoning disposes of a category the conventional tooling layout quietly misfiles.
**`bin/guard` — the
hook that runs on every agent tool call — is a grant enforcer, so it belongs with authority, not in
the project's `bin/`.** The excuse that saves gates does not save it: a weakened gate is caught by
review before it can authorize anything, but a guard weakened at step N authorizes step N+1
*immediately*, before any review exists. That is the "self-authorizing bound" failure verbatim.

So guard's rules come from the companion repo and are applied by the arena when it launches the
agent harness. The same goes for the harness's tool allowlist and its MCP mount list: if that
configuration lives in the project tree, an `implement` step edits it and mounts itself a new
server.

**MCP servers split by what they do rather than by what they are.** A server that grants reach —
shell, network, a database — is capability, and per [One binary per
project](#one-binary-per-project) capability comes from the environment: the arena provisions it,
the companion repo grants it per tool. A server that only exposes knowledge *about* the tree —
compiler introspection, symbol lookup — is gate-shaped and legitimately comes from the tree. Which
gives one rule that disposes of the whole category:

> **A tree-provided MCP server may only ever be granted read.** If it needs write, it is not a
> knowledge server, and it cannot come from the tree.

**A network-reaching MCP server collides with the [outbound-to-Reactor-only
invariant](design.md#deployment-topology--server-governor-runner)** — the property that lets an
arena run with tightly restricted egress
and one trust path to verify. The consistent resolution is the one already used for flow delivery:
the server runs outside the arena and is **proxied through Reactor**, which mirrors rather than
redirects. Same invariant, same single trust path, and the proxy is the natural place to enforce the
per-tool grant and log the calls — which supplies the post-hoc audit layer on the one resource where
prevention is hardest. See [the capability
vocabulary](design.md#the-capability-vocabulary) for how the grant is expressed.

### Visibility is a constraint, not a detail

`workspace` and `tracker` are **private**; `reactor`, `promise`, and `base` are public. That
asymmetry constrains the layout directly:

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

### The shared layer is one repo

**It is [`promise-language/base`](https://github.com/promise-language/base)**, public, and it
consolidates what was previously spread across `workspace` (delivery, provisioning, arena setup)
and two earlier public repos — dev-tooling conventions, and the flow common library and gate SDK.
Those are prior art: nothing is ported from them and nothing stays compatible with them. A BASE
layer already existed in embryo as `workspace` — still private, and
carrying exactly the two-layer mixture described above, generic machinery beside `projects/promise/`
and `projects/tracker/`. The work is less "create a repo" than "name the layer that exists, move the
per-project halves out, and port it to Promise".

**One repo, several modules.** Subdirectory module addressing
([P12](design.md#platform-requirements--requested-of-promise), landed in head and shipping with the
next release cut) puts the subpath on the *named* require entry, so several entries may point at one
URL with different subdirs. That
is what makes consolidation work rather than merely tolerable: Reactor depends on the wire types
alone, a flow on the wire types plus the common library, and a project's gate on the gate SDK alone,
none of them pulling the others into their compilation.

**What it costs is fetch, not compilation.** Addressing is not fetching, so until partial clone
([P13](design.md#platform-requirements--requested-of-promise)) lands, a consumer wanting only the
gate SDK still clones the whole repo with full history. Shared fetch amortizes that across modules
taken from the same repo without shrinking the first one. This is a known, deferred delivery cost
rather than an open problem: the fix belongs in the layer that already mirrors flow releases instead
of redirecting runners to a code host, and the trigger for building it is the first gate author
outside the organization.

**Base carries an epoch obligation the other repos do not.** Transitive dependencies resolve at the
*consumer's* epoch, and base is consumed by repos it does not control — every companion repo, and
for the gate SDK any third-party project. So base must stay compatible with the widest epoch span
across all of them, and so must everything base itself depends on. The practical consequence is that
**base should depend on as little as possible**, since each dependency narrows the range of
consumers it can serve.

The [white paper](../WHITEPAPER.md) could move here too — the methodology is not the orchestrator —
at the cost of breaking public inbound links from promise's README and the generated
`promise-lang.org/base` page. Left where it is for now.

## Dev tooling

"The tools" is one word for four different things with four different owners, and conflating them is
what makes "who writes `bin/verify`?" sound harder than it is. They separate by **what each does to
the boundary**:

| | Examples | What it does | Comes from | Prescribed by |
|---|---|---|---|---|
| **Gates** | tests, vet, coverage | *measures* the tree | the tree | BASE prescribes the **contract**, never the roster |
| **Dev tools** | format, build, a fixer | *edits or checks* the tree for a human or an agent | the tree | nobody — project convention |
| **Bounds** | guard, pre-commit, tool allowlists | *constrains* the agent | the companion repo | BASE, mandatorily |
| **Capabilities** | MCP servers, shell, egress | *grants* the agent reach | the arena | BASE, mandatorily |

The first two rows are the project's business and the last two are authority — see [Bounds are
authority, not tooling](#bounds-are-authority-not-tooling).

### What BASE actually requires of a project

**Two things, and nothing else.** A command that enumerates the project's gates — by convention
`bin/gate list --json`, emitting [the
manifest](#gate-discovery--the-project-declares-reactor-discovers), plus the JSON envelope each gate
writes. And a [`.base/` directory](#the-base-directory--how-a-project-names-its-setup) naming which
companion repo defines the project's process and which deployment orchestrates it. That is the
entire mandatory surface: one command, one config file.

BASE deliberately never names `bin/format` or `bin/vet`, and it must not: [the polyglot
boundary](#language) means a Rust project satisfies the same contract with `cargo fmt` and `cargo
clippy` and no `bin/` at all. **The manifest is the indirection that makes the roster private to the
project.** So the answer to "who prescribes the layout of the tools" is that nobody does, by design —
what BASE prescribes is that a project can *enumerate* whatever layout it has.

### Who writes them

Three layers, and only the middle one is negotiable:

1. **The contract** — manifest, envelope, guard protocol, capability grants. BASE's, mandatory,
   language-neutral, tiny.
2. **A blueprint supplying a default roster** — BASE's, *optional and opinionated*, specified in
   [dev-tooling.md](dev-tooling.md). It is where `verify` / `format` / `vet` / `test` / `guard`
   legitimately exist **as names**: a scaffold a new project starts from. A project that ignores it
   and prints its own JSON is equally compliant. **A blueprint that reads as normative is a
   blueprint being misused** — the earlier Go one did, which is the mistake this layer inherits the
   lesson from rather than the machinery.
3. **The implementations** — the project's. For a project run under BASE the honest answer to "who
   creates them" is that **the bootstrap is human and the rest is backlog**: somebody hand-writes
   the manifest command and one gate, after which "add a `vet` gate" is an ordinary work item an
   `implement` step resolves.

Two of those names come off the project's list entirely: `verify` is [derived from the
manifest](#verify-is-derived-not-declared) and ships in the gate SDK, and `guard` is
[authority](#bounds-are-authority-not-tooling) and comes from the companion repo. What a project
actually authors is gates, fixers, and whatever dev tools it wants for itself.

### How they are built

The Go blueprint this repo still uses (`./make` → `bin/`, source-hash staleness checks, committed
trampolines) exists almost entirely to work around `go run`, and stops being necessary once tools
are written in Promise. See [dev-tooling.md](dev-tooling.md).

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

  **That tradeoff is only survivable if versions can read each other's leavings.** Per-step
  resolution means step 3 may run under one flow version and step 4 under the next, so **persisted
  step state written by one version must be readable by the next**, and the flow↔Reactor wire must
  tolerate the same skew — see [a shared module is not a shared
  version](design.md#a-shared-module-is-not-a-shared-version). Without both, "picked up by the
  remaining steps" describes a corruption rather than a recovery.

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
project that makes the [Promise tooling model](dev-tooling.md) load-bearing rather than a
convenience, and it puts three requirements on it. (A gate in another language faces the same three
questions in its own toolchain — they are properties of "build from the tree on every run", not of
Promise.)

1. **Compile caching must be real**, or every gate run pays a full compile of the gate before doing
   any work. In a fresh ephemeral arena the cache is cold by definition, so the arena image should
   ship or mount a warm cache — arena-provisioning work, not language work.
2. **`promise run` must *exec* the compiled binary, not stay resident as its parent.** The runner
   waits on the gate process and kills it on timeout ([nothing runs
   unwatched](design.md#nothing-runs-unwatched)); an interposed parent turns signal delivery and
   exit-status propagation into a wrapper problem, and leaves the real gate as an orphan when the
   deadline lands.
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

## Step resolution — steps dispatch themselves

> **The contract a flow implements.** Reactor's half of it is
> [Step execution](design.md#step-execution--reactors-half).

### There is no plan

**No per-item step plan is built, on the first run or any later one.** The resolver walks the flow's
declared steps and asks each to resolve the item; the set that actually runs is not knowable until
the item is resolved, because it depends on what is already true.

The reason is not elegance. **The flow version resolves [per step](#the-principle)** — deliberately,
so a flow bug found mid-resolution is fixed outside and picked up by that item's remaining steps.
A stored plan cannot survive that: the moment the flow changes it names steps that may no longer
exist, in an order that may no longer hold. A design that promises async flow fixes and also stores
a plan has committed to keeping a cache coherent with a thing it explicitly allows to change
underneath it. Self-dispatch has no cache to invalidate.

It also makes resume trivial. Re-entering the loop needs no restored position, which is what
[invariant 4](#4-an-items-work-binds-to-an-arena-and-carries-its-state-forward) wants of a resumed
step anyway.

**"No plan" is not "no flow definition."** The flow still declares its steps, their order, and their
eligibility — that is what [`describe`](design.md#what-a-flow-declares-and-what-is-declared-about-it)
emits. What is removed is the per-item *instance* of that plan, not the shape it is drawn from.

### The outcomes belong to three layers

Flattening these into one list is what makes the protocol ambiguous, because they answer different
questions.

**`check` — is my postcondition already true?** Deterministic, reads artifacts, and cheap.

| | Meaning |
|---|---|
| `satisfied` | my artifact exists and verifies; skip me |
| `unsatisfied` | I have work to do |
| `blocked` | I cannot even be evaluated until something else changes |

**`run` — do the work.** Invoked only for the first `unsatisfied` step.

| | Meaning |
|---|---|
| `advanced` | I produced my artifact; re-enter the scan |
| `blocked` | rerun when *&lt;condition&gt;* — the same [edge vocabulary](design.md#an-edge-names-a-target-and-a-condition-never-a-version) used across projects, plus "a human answered" |

A `run` that produces neither an artifact nor a block is **not an outcome**. The step is simply not
done, and [invariant 6](#6-a-steps-completion-is-a-verified-artifact) returns it to resolution with
the verification failure as context. A `run` that blocks **must leave a
[checkpoint](#a-step-may-carry-work-forward-without-claiming-completion)**, or state that it has
nothing to carry — otherwise the answer it waited for arrives to a step that starts over.

**The scan — what happened to the item.** These are the resolver's conclusions, not any step's:

| | Meaning |
|---|---|
| `complete` | every declared step is satisfied; the item is resolved |
| `handoff` | the next unsatisfied step exists but **this role may not run it** |

**`handoff` is the outcome most easily lost, and losing it is expensive.** A contributor scanning
reaches `merge` and stops. That is neither completion nor blockage: folding it into the first
silently drops work, and folding it into the second sends remediation after a blocker that does not
exist. It is also how the trust ladder actually advances — *bracketed by trusted gates* is this
outcome, mechanized.

### `check` and `run` are separate for a reason that is enforceable

If a satisfied step answers by starting an agent to conclude "I was already done", a scan across
five satisfied steps costs five agent sessions. Keeping the *judgment* with the step while making
the *scan* cheap needs two entry points rather than one.

> **`check` runs with no model account injected.** It cannot spend tokens however it is written.

That is [credential scoping](design.md#where-it-is-enforced) rather than a convention a step is
asked to honor — the same reason nothing else here is left to good behaviour. It also forces
`check` to stay what it should be: a deterministic read of durable state.

### Context is assembled, never accumulated

A step's context — what earlier steps produced, what previous runs of *this* step did, and why it is
being run again — is **derived from the durable artifacts at dispatch**, not carried forward from
run to run. That bounds growth, and it means a resumed step reconstructs identical context from the
record rather than from anything that died with its process, which is what
[invariant 4](#4-an-items-work-binds-to-an-arena-and-carries-its-state-forward) already assumes.

### Termination

- **Forward only.** A step may report satisfied and let the scan continue past it; it may never send
  the scan backward. With the declared order fixed and the step count finite, a scan terminates.
- **Progress is a new verified artifact**, which is the same predicate the
  [grant ladder](design.md#the-grant-ladder) extends on. A `run` that produces none did nothing the
  previous attempt had not, and that is the ordinary
  [loop-detection](design.md#every-attempt-must-make-progress) case — park it rather than trying
  harder. **A block is the one exception, and only when its
  [checkpoint](#a-step-may-carry-work-forward-without-claiming-completion) advanced**: work was done
  and recorded, it simply cannot finish yet. A block whose checkpoint stood still is the loop case
  like any other.

### What this fixes, and what it does not

Worth stating plainly, because the change is easy to oversell.

**It fixes** the class where a step's own view of completion and the flow's bookkeeping disagree —
a step that cannot satisfy an artifact predicate the flow imposed on it, and a step that reports
done while the flow's record says otherwise. That class regenerates: individually patchable
instances of it recur for as long as the judgment lives outside the step.

**It does not fix** a step that reports completion falsely (that is invariant 6), non-idempotent git
handling, a wall-clock budget racing work whose cost the step does not control, or a killed process
leaving a live child. Those are real and they are orthogonal; a redesign that claimed them would be
taking credit for other people's fixes.

## The flow contract

A flow is a binary. This is everything it implements and everything it may ask for; nothing else
crosses either boundary. Both are narrow on purpose — [the runner is the local trust
boundary](design.md#the-runner-is-the-local-trust-boundary) and a flow is a guest inside it.

### What a flow implements

Three subprocess entry points, and no others:

| Command | Returns | Notes |
|---|---|---|
| `describe` | the flow manifest | item types, steps and their order, eligibility, `serialized_by`, fresh-session and arena-independent hints, and how each step's artifact is verified |
| `check <step>` | `satisfied` · `unsatisfied` · `blocked` | launched **with no model account**, so a `check` that wanted to spend could not |
| `run <step>` | `advanced` · `blocked` | the arena's account is present |

`describe` is deliberately symmetric with `bin/gate list --json`: a subprocess that emits a manifest
the system validates. One discovery mechanism serves both halves.

**A step need not run an agent.** `run` may complete without ever calling `agent.run` — the step
that verifies ratchets and amends a commit is deterministic and better for it, since judgment about
where a quality floor sits is judgment nobody wants delegated. A mechanical step costs no model
account, meters nothing, and asks the [grant ladder](design.md#the-grant-ladder) no questions.

### What the runner supplies

Assembled environment, not arguments a flow could forge: the item and step it is running, the
worktree path, arena context, and the loopback endpoint with a per-run credential. **Never a model
credential** — the runner runs the agent, so the flow has nothing to spend with.

### What a flow may ask for

Every operation goes to its runner over loopback, and the runner forwards what belongs to Reactor
with the step's attribution stamped. A flow never addresses Reactor, the code host, or a model
endpoint itself.

| | Operation | Notes |
|---|---|---|
| **Item** | `item.read` | within [read scope](#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped) |
| | `item.create` | filing into another project — [the ability to ask](#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped), never to write |
| | `item.annotate:<kind>` | plan · inspection · review · note · **question** |
| | `item.state` | close as [resolved, declined, or moved](design.md#the-states-and-what-they-belong-to) |
| | `routing.set` | `flow:` labels and assignee — [its own capability](design.md#the-capability-vocabulary) |
| **Work** | `artifact.submit` | own step only; Reactor verifies before recording completion |
| | `checkpoint.write` | own step only |
| | `hold.place:<kind>` | `blocked`, `waiting`, or `parked` |
| **Agent** | `agent.run` | one prompt, one completed run; the runner mounts tools, holds credentials, and meters |
| **Gates** | `gate.run` · `gate.results.read` | the runner executes; the flow asks |
| | `baseline.update` | mechanical — verifies the ratchets and writes the new values; the [baseline file is a denied path](design.md#the-capability-vocabulary), so this is the only way it changes |
| **VCS** | `vcs.push:branch:own` · `vcs.pr.create` | proxied — a grant over what the runner does on the flow's behalf, not permission to open a connection |
| **Engagement** | `article.post` · `article.resolve:own` | [feed articles](engagement-feed.md#the-article) |

Local git inside the worktree is ordinary filesystem work bounded by the step's
`write:<glob>` grant, and needs no operation here. Only what reaches origin is proxied.

### An operation whose halves must both happen is one call

Two places where splitting would make a stated rule enforceable by convention only:

- **Reporting `blocked` carries the checkpoint.** *Blocking requires a checkpoint* is otherwise a
  rule two separate calls can half-satisfy — and the failure mode is exactly the lost work the
  checkpoint exists to prevent.
- **Asking a question is one operation**, the annotation and the `waiting` hold together. Half of it
  is either a question nobody is waiting on or an item waiting on a question that does not exist.

### What a flow may not do

Not conventions — the absence of an operation, an environment variable, or a route:

- **Reach the network.** [`net.egress` defaults to none](design.md#a-flow-has-no-network).
- **Spawn an agent.** There is no credential to spawn one with.
- **Write an `answer`.** An answer is a principal's; a flow that could write one would answer its
  own question, and [an answer is authority rather than
  annotation](design.md#the-capability-vocabulary). A principal may still [ask an agent to *draft*
  one](engagement-feed.md#questions-answers-and-history) — the authority stays theirs, and so does
  the acceptance.
- **Clear a `parked` or `manual` hold.** Clearing a fault it caused, or taking back work a person
  took over, are exactly the two the [separate clear
  grant](design.md#the-capability-vocabulary) exists to withhold.
- **Run outside a runner.** [Including when a developer runs
  it](design.md#there-is-no-way-to-run-a-flow-except-through-a-runner).

### Three things this contract settles

1. **A flow is handed its item; it never claims one.** Reactor decides eligibility and dispatch, and
   the runner takes the claim lease before the flow is invoked for a specific item and step — so
   `item.claim` is not an operation here at all. What loops is the runner's long-poll, not the flow.
2. **A step never clears a hold.** A `blocked` hold clears when its edge resolves, a `waiting` hold
   when an answer lands, and `parked` and `manual` need a person by definition — so no case remains,
   and `hold.clear` belongs only to humans and to Reactor.
3. **Baselines are the step's and exceptions are a human's.** `baseline.update` is mechanical and
   happens at landing; an exception is permission to regress a ratchet, which is asked as a pinned
   [question](engagement-feed.md#questions-with-deadlines) and answered by a role that carries the
   grant. Neither is ever an agent's judgment.

## Gate discovery — the project declares, Reactor discovers

Tracker required each gate to be entered by hand into server config (name, command, schedule, host
filter, metric directions, ratchet caps). That doesn't scale to a multi-project Reactor and forces
a maintainer to mirror project knowledge into the server. The relationship is inverted here: **the
project declares its gates; Reactor discovers them.**

**The contract.** A project exposes a single command — convention: `bin/gate list --json` — that
emits a manifest describing every gate it offers plus a global preflight command. The manifest is
the source of truth for gate *identity*, *runtime*, *eligibility*, and *metric semantics*.

### Preconditions and monitors are different things

The manifest as originally drafted could only describe **monitors**. Its `schedule` vocabulary —
`every <dur>`, `daily`, `weekly`, `after-every-commit`, `manual` — is entirely retrospective;
`after-every-commit` says *after*, measuring a commit that already exists. There was no way to
express "this transition does not happen unless this is green", which is the only thing verify is.

The two kinds differ in nearly every property:

| | **Precondition** | **Monitor** |
|---|---|---|
| When | before a transition | on a schedule |
| Failure means | the transition is refused | a bug is filed, the baseline ratchets |
| Consumed by | the step attempting the transition | Reactor's scheduler and ledger |
| Latency | on the critical path | irrelevant |
| Metrics | compared against the baseline; never move it | compared, and ratchet the baseline on success |
| Host | must be the host doing the transition | any eligible arena |

**Both read the baseline; only landing moves it.** A precondition that ignored the baseline could
not catch a coverage or test-count regression, which is exactly what it is for. But a candidate tree
that has not landed must not raise the bar for everyone else, and a rejected one must not lower it.

> **There are two kinds of baseline, and which one a metric gets is decided by when its measurement
> is available.**

| | Measured | Baseline lives | Moved by | On regression |
|---|---|---|---|---|
| **Precondition** | during resolution | **in the tree** | the step that lands the change | the change does not land — **prevented** |
| **Monitor** | on a cadence, after landing | **with its history**, server-side | Reactor, on a run against landed trunk | an item is filed — **detected and undone** |

Some measurements simply cannot be taken in the resolution path: a stress run that takes two hours,
a WASM binary-size check that runs once a day. Their results arrive long after the change landed and
often describe a commit several behind, so there is nothing left to amend them into and nothing to
refuse. Forcing them into the tree would mean either blocking every landing for two hours or
committing a number about the wrong commit.

That is the same split as [preconditions and monitors](#preconditions-and-monitors-are-different-things)
themselves, and the same [materiality test](design.md#where-it-is-enforced) the authority model uses:
a restriction counts as enforced if a violation is **prevented**, *or* **detected and undone**. A
monitor baseline takes the second path because the first is not available to it.

**A monitor baseline needs tolerance; a precondition baseline mostly does not.** A test count is
exact and moves only when someone writes a test. A stress-run duration or a binary size is a
*measurement*, with variance — so raising the bar on any single improvement means one lucky run on a
quiet machine ratchets it, and every honest run afterwards reads as a regression. Monitor ratchets
therefore move on sustained improvement rather than on a single sample, and their tolerance is
deployment config alongside the [ratchet cap](design.md#gate-execution--reactors-half).

The rest of this section is about the first kind.

> **A precondition's baseline travels with the tree, and is moved by the step that lands the
> change — never on a schedule.**

That is not a convenience. **A baseline raised out-of-band makes in-flight work uncommittable**:
every arena branched before the raise carries the older file, and their landings then either
conflict on it or fail a ratchet they were never given a chance to meet. A monitor updating trunk's
baseline on its own cadence would do exactly that, to every arena at once, on a timer nobody
correlated with the failures.

Moving it at landing satisfies the asymmetry instead of working around it: the update is amended
into the commit that becomes trunk, so a raise that never lands never happened, and a rejected
candidate takes its baseline change with it.

**And the update is mechanical, not a judgment.** Once the gates pass and the ratchets verify, the
new values are written and the commit amended. Nothing is asked of an agent, which matters because
this is the one place where an agent's judgment would be a hazard rather than a feature: a model
that can *decide* what the quality floor is has been handed the floor.

**So the agent never edits the baseline — it invokes an operation that updates it.** The file is a
[denied path](design.md#the-capability-vocabulary) in every tree-write grant, exactly as that
carve-out already proposed, and the deny is safe only because this sanctioned path exists.

So a gate gains a `blocks` field, orthogonal to `schedule` — a gate may do both, blocking a push
*and* running every four hours to catch flakiness and environmental drift. **`blocks` values are
drawn from the [VCS capability vocabulary](design.md#the-capability-vocabulary)**
(`commit`, `push:branch`, `push:origin`, `pr.create`, `pr.merge`) rather than a parallel set of
transition names, so "which gates block this action" is a lookup keyed on the same strings the
grants are written in.

**Once preconditions hold, monitors have a narrow job.** If `push:origin` is blocked on green, trunk
is green by construction, and the scheduled set exists only for what preconditions structurally
cannot catch: cross-platform, long-running, flaky-under-load, and dependency rot that changes with
no commit at all. That is a real job, but a small one — the opposite of how a schedule-first manifest
presents it.

#### Preconditions are enforced at the boundary

A precondition is checked at up to three places, and only the last of them is authority:

1. **Any step, advisorily.** A step may run a transition's gate set early to learn its fate. This
   refuses nothing — see [Commit-time verify is a
   prediction](#commit-time-verify-is-a-prediction-not-a-gate).
2. **Reactor, before dispatching the transition step.** It runs the gates naming that transition in
   `blocks` and does not proceed if they fail. This is the ordinary path, and the one that turns a
   failed precondition into a recorded step outcome rather than a rejected push.
3. **The code host, unconditionally.** Branch protection refuses the push whatever ran anywhere
   else.

Layer 3 is what makes the invariant material, because a local git hook is bypassable — an agent that
can run `git` can run `git --no-verify`. So **a precondition enforced only by a local hook is
advisory**, and by the [materiality test](design.md#where-it-is-enforced) must be labelled as such.
Layers 1 and 2 exist for speed and attribution, not authority.

Whatever runs at layers 1 and 2 must be the gates the manifest declares, never a hand-kept list, or
the early answer stops matching the late verdict — see
[invariant 2](#invariants-and-properties-are-enforced-differently).

**`blocks: ["commit"]` is for invariants, not properties.** Refusing a commit is right when the gate
checks something never permissible in any intermediate state — a committed binary, a secret, a
suppression tag. Blocking a commit on a whole-tree property is the mistake invariant 2 rules out.

#### Commit-time verify is a prediction, not a gate

`push:origin` is the enforcement boundary; committing broken work is legitimate, because a commit is
how an agent records a floor to fall back to. But a commit that fails verify is *doomed* — it will
be rejected at push — and saying so early is worth doing.

That is a prediction, and it is reliable for exactly one reason: **it is literally the same gate set,
not a lighter approximation of it.** A cheaper commit-time list would be a heuristic, and a heuristic
that says "you're fine" when the push will reject is worse than running nothing, because it converts
a known cost into a surprise. This needs no manifest vocabulary at all — any step may run the
push-blocking set at any point to learn its fate.

Running it early buys more than wall-clock. At commit time the failure is unambiguously the agent's
own; after `integrate` has rebased onto a moved trunk, the same failure may belong to the interaction
with someone else's landed change. Those are different remediation problems with different owners,
and verifying before integrating is what keeps them distinguishable.

**Scope, which must be stated rather than assumed.** "A commit that fails verify is rejected later"
is true under *every-commit-green* semantics, but a push verifies a **branch**, and a later commit
can fix an earlier one. Bisect is how a monitor's finding gets localized, and bisect over a history
with broken intermediates returns noise — so every-commit-green is worth wanting here specifically.
It need not be paid for: **squash on `integrate`** makes the pushed range a single commit, at which
point head-only and every-commit are the same thing and the question stops existing.

### Manifest shape (v1, JSON)

```json
{
  "schema_version": 1,
  "project": "promise",
  "targets":        ["linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64", "wasm32"],
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
      "blocks":          ["push:origin"],
      "schedule":        "every 4h",
      "serialized_by":   ["host:cpu"],
      "tags":            ["tests", "host"],
      "metrics": [
        { "name": "test_count",    "type": "int", "direction": "up",   "mode": "enforced",      "cap": 10000 },
        { "name": "test_failures", "type": "int", "direction": "down", "mode": "enforced",      "cap": 0     },
        { "name": "leak_count",    "type": "int", "direction": "down", "mode": "enforced",      "cap": 0     },
        { "name": "excluded_count","type": "int", "direction": "down", "mode": "informational"               }
      ]
    },
    {
      "name":            "promise-wasm-tests",
      "command":         "bin/gate test --wasm",
      "host_os":         ["linux", "darwin"],
      "target":          "wasm32",
      "timeout":         "20m",
      "blocks":          ["pr.merge"],
      "tags":            ["tests", "wasm"],
      "metrics": [
        { "name": "test_failures", "type": "int", "direction": "down", "mode": "enforced", "cap": 0 }
      ]
    },
    {
      "name":            "promise-format",
      "command":         "bin/gate format",
      "fix":             "bin/format",
      "host_os":         ["any"],
      "timeout":         "2m",
      "blocks":          ["push:origin"],
      "tags":            ["format"],
      "metrics": [
        { "name": "unformatted_files", "type": "int", "direction": "down", "mode": "enforced", "cap": 0 }
      ]
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `schema_version` | Major version; Reactor refuses unknown majors. |
| `targets` | What the product is built **for** — the set [invariant 1](#what-every-platform-means) means by "every platform". A single entry makes the project single-target and the matrix a matrix of one. |
| `preflight` | Optional global setup command Reactor runs after a fresh checkout, before any gate (build the gate binary itself, sync submodules, sanity-check the tree). OS-dispatched. |
| `gates[].name` | Stable id; keys metric history and baselines. **Must be unique within the manifest.** |
| `gates[].command` | Exec line. OS-dispatched. |
| `gates[].fix` | Optional deterministic remediation command. OS-dispatched. **Never run by the gate runner** — see [The check/fix pair](#the-checkfix-pair). |
| `gates[].host_os` | `linux` / `darwin` / `windows` / `any`. Eligibility filter. |
| `gates[].target` | Which entry in `targets` this gate's verdict speaks for. Omitted ≡ the host it ran on, which is the common case; state it when they differ, as for a `wasm32` gate running on a Linux host. |
| `gates[].host_arch` | Optional `amd64` / `arm64` filter — lets a project target "linux arm64" separately from "linux amd64" without a target-triple grammar. Omitted ≡ any. |
| `gates[].timeout` | Duration (`30m`, `2h`). Bounds *work*, not queue wait — [invariant 3](#3-serialization-is-declared-and-waiting-for-it-is-not-work). |
| `gates[].blocks` | Transitions this gate is a precondition for, from the [VCS capability vocabulary](design.md#the-capability-vocabulary). Omitted ≡ blocks nothing (a pure monitor). |
| `gates[].schedule` | Monitor cadence: `every <dur>`, `daily`, `weekly`, `after-every-commit`, `manual`. Omitted ≡ never scheduled (a pure precondition). |
| `gates[].serialized_by` | Named exclusions this gate needs, each `<scope>:<leaf>` with scope in `project` / `host` / `arena` / `global`. Declared statically so Reactor can acquire in a canonical order and exclude the wait from the deadline. |
| `gates[].tags` | Free-form; attached to auto-filed bugs. Also the selector for verify subsets (`verify --tags wasm`). |
| `gates[].metrics[]` | One spec per metric the gate emits. |

**A gate must declare `blocks`, `schedule`, or both.** One that declares neither can never run, and
the manifest validator should reject it rather than let it sit inert.

**OS-dispatched commands.** `preflight`, `gates[].command`, and `gates[].fix` each accept either a
**string** (used on every OS) or an **object**
`{ "default": …, "linux": …, "darwin": …, "windows": … }` — the host-OS key wins, `default` is the
fallback. OS keys use the same vocabulary as `host_os`. A bare
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

### The check/fix pair

Some gates have a deterministic remediation sitting three feet away — formatting is the obvious
case — and a manifest that models checkers only cannot say so. The consequence is concrete: when a
format gate fails, the flow's sole remedy is *ask the model to fix it*, which is expensive,
nondeterministic, and prone to producing near-miss formatting that fails the same gate again. That
cost lands on the critical path, where it is least affordable.

Hence `fix`, with a contract the project must uphold:

> **`fix` is idempotent, touches only what `command` would flag, and `fix` followed by `command`
> passes.**

BASE cannot prove that, but it can *test* it — a meta-gate that dirties the tree, runs `fix`, and
asserts the checker goes green catches the two-implementations drift this exists to prevent.

**The stronger form, where the gate admits it: define the checker as the fixer's dry run** —
`check ≡ (fix produces no diff)`, the `gofmt -l` shape. Divergence then becomes structurally
impossible rather than merely discouraged. That only works where failure is expressible as a diff,
so formatting yes, vet and tests no; paired declaration is the general fallback.

**The gate runner never invokes `fix`; the flow does.** The manifest only declares *where* the fixer
is. That placement looks like it strains [the principle](#the-principle) — the fixer comes from the
tree, and it *modifies* the tree — so it is worth being precise about why it does not. The principle
constrains **resolution logic**, which must live outside the worktree because fixing it mid-flight
would contend with the work in flight. A fixer is not resolution logic; it is a tool the flow
invokes, exactly as it invokes the compiler, and what it does is defined by the tree's own
configuration. Deciding *when* to run it stays outside, with the flow.

### A gate never modifies the tree

"A gate measures the tree" is stated as the reason gates come *from* the tree. It is also a
constraint on what a gate may *do*, and that half has to be enforced rather than assumed — a
formatting gate that formats, rather than reporting, is the obvious way to get it wrong, and it is
worse than it looks because the formatter is itself a tool in the tree. The thing being measured
and the thing doing the measuring start changing each other.

> **A gate may not modify tracked content. Not as a side effect, not as a convenience, not to fix
> what it found.**

Remediation belongs to the [`fix` command](#the-checkfix-pair), invoked by the flow as a step, never
by the gate runner. A gate that repairs what it measures destroys the only signal it exists to
produce: a green result no longer distinguishes "was correct" from "was made correct", and the
change it made is attributed to nobody.

**Enforced at two layers**, in the design's usual shape — preventive where the platform allows,
detective everywhere:

- **A read-only worktree mount** for the gate's process, which is already named as a [sandbox choke
  point](design.md#where-it-is-enforced). Where available this makes the rule unbreakable rather
  than merely stated.
- **A post-run check that no tracked file changed**, which is portable and catches what the sandbox
  cannot.

**There is no opt-out, and an earlier draft's `allow_dirty_tree` is gone.** The case it existed for
— a gate that legitimately drops build output somewhere — is already covered by the tree's ignore
rules, so the field only ever duplicated a declaration that exists, is versioned with the code, and
is read by everything else anyway.

Worse, it licensed the failure it looked harmless against. **Untracked residue is how a gate goes
green on state that a fresh clone will not have**, which is exactly what
[invariant 1](#1-origin-is-always-green-on-every-platform) means by verify depending on no local
state for correctness. Trunk then passes here and fails there, for reasons nothing recorded.

Failing instead is the useful outcome: it forces the residue to be named in the ignore rules, where
it is reviewable and travels with the tree — rather than waved through by a manifest flag that says
*something* was left behind without saying what. **A step and a gate are held to the same
postcondition**, and neither has an exemption.

### verify is derived, not declared

**`verify` is not an entry in the manifest. It is a client of it.**

Declaring it as a gate would break two ways at once: its metrics would collide with the child gates'
— `test_failures` counted twice, history muddled — and the project would maintain two lists, what
verify runs and what actually blocks the push. Those lists drifting *is* the late-rejection failure.
Verify comes back green, the push is refused by a gate verify did not know about.

Derive it instead, per transition:

- `verify` ≡ run the gates declaring `blocks: ["push:origin"]`, eligible on this host
- pre-merge ≡ the gates declaring `blocks: ["pr.merge"]` — where the cross-platform matrix lives,
  since it cannot block a push from a single host

The local check and the rejecting check are then the same command line by construction, because both
are read out of one declaration. Sameness stops being a discipline the project maintains and becomes
a property of there being one source — which is the same shape as the check/fix pair above and as
[declare once, enforce at many points](#invariants-and-properties-are-enforced-differently).

It also dissolves the composite question. Verify needing to be vet + format + tests is not a new
gate *kind*; it is a selector over the existing set, and `verify --wasm` is a `tags` selector.

**Consequence for ownership: a derived verify is identical code for every project**, so it ships in
the gate SDK and nobody authors it per project. Projects author gates and fixers; the runner over
them is BASE's.

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
3. **Execute.** For monitors, Reactor's scheduler picks eligible gates by host OS × arch ×
   deployment overrides; for preconditions, the transition being attempted selects them via
   `blocks`. Either way Reactor acquires the gate's `serialized_by` exclusions in canonical order,
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

## Open questions

What is genuinely undecided in the project-facing layer. Everything else here is a statement about
the system.

1. **Where the reusable machinery finally consolidates.** It is spread across several repos today,
   and the generic/specific boundary is firm while the packaging is not — see
   [What lives where](#what-lives-where).
2. **Whether per-project definitions move out of the shared repo.** A shared BASE repo accumulating
   a `projects/<name>/` directory per orchestrated project is where the domain-agnostic claim leaks;
   moving them means adding a project touches no shared repo at all.
3. **Where the methodology documentation lives.** The [white paper](../WHITEPAPER.md) could move
   beside this layer rather than beside the orchestrator, at the cost of breaking public inbound
   links.
