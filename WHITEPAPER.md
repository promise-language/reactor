# Bounded-Autonomy Software Engineering

*A white paper on building and maintaining large, complex software with AI agents
under bounded autonomy — agents free over the HOW, held to a human-owned design
specification. [Reactor](https://github.com/promise-language/reactor) is one
implementation of this approach.*

---

## Abstract

The common verdict on AI-written code is that it tops out at "slop": fine for a
snippet or a prototype, but unable to build — let alone *maintain* — a large,
complex system. The caveat usually buried in that verdict is the real story:
*with a human in the loop cleaning up after every step*, agents already build
large systems. What they cannot yet do is build and *maintain* one without a
person on the critical path. This paper argues the ceiling is real but
misattributed. It is not a ceiling on the
*model*; it is a ceiling on the *system around the model*. Give an agent durable
intent, a mechanical definition of "correct," an automated resolution loop, an
orchestrator that holds state across a fleet, and a human who is engaged *by
design* — the system runs autonomously by default and escalates to a person
deliberately, keeping them mostly off the critical path rather than strapped into
it — and the work that survives is large, complex, and maintainable.

We call that discipline **Bounded-Autonomy Software Engineering (BASE)**, and the
term is meant precisely. **The spec is the bound:** the agent has genuine autonomy
over the *how*, *bounded* by two things the human owns — durable **intent** (the
what and why) and a mechanical definition of **quality** (gates and ratcheting
baselines). Together those are the **design specification** the software must
satisfy to be correct and fit for purpose. This is not the safety/guardrail sense
of "bounded autonomy" from the AI-governance literature — the bound is not a static
rail against harmful behavior, it *is* the spec. And the spec is **fluid**: a
living artifact that adapts as a complex system surfaces internal contradictions,
as a step proves infeasible, and as external requirements shift across the
software's lifetime. When the spec proves contradictory, infeasible, or outdated,
that triggers a **renegotiation of the bound** — escalated to the human, by
design, not a failure (the *Engage* subsystem of §3). This is the disciplined
middle between *assisted* development and unbounded *"vibe coding"* — §1 draws the
line. We make a falsifiable claim about it: this is a problem of *building the
right scaffolding*, not a *capability* problem waiting on a better model. The
existence proof is concrete — the [Promise language](https://github.com/promise-language/promise)
compiler, standard library, and catalog were built and are maintained this way,
by agents under human design direction, on a single $200/month subscription.
The Promise language was built with a private implementation of BASE. **Reactor**
is the open-source orchestration system that will run this same loop; it is at
present an early-stage project — a design doc, not yet a working tool. This paper
defines the methodology; [`docs/design.md`](https://github.com/promise-language/reactor/blob/main/docs/design.md) sketches Reactor's
architecture.

---

## 1. Bounded autonomy, not assisted

BASE is not a coding assistant, and it is not "vibe coding." It is the disciplined
middle. The distinction is not the model or the prompt — it is **where the human
sits relative to the build loop, and what bounds the work.**

- **Assisted development** (copilots, chat-in-the-IDE) keeps the human *in* the
  build loop: the agent proposes, the human accepts and drives. Throughput
  is bounded by human attention, one keystroke at a time. There is no real
  autonomy — the human is on the critical path for every step.
- **"Vibe coding"** removes the human but also removes the bar: generate
  something that runs once, ship it, hope. It is **unbounded autonomy** — no
  intent it must hold to, no quality it must clear — and it produces exactly the
  slop the critics describe.
- **Bounded autonomy** keeps the autonomy *and raises the bar*: the agent defines,
  implements, tests, and gates the work, free over the *how* — but bounded by what
  the human owns, durable **intent** and a mechanical definition of **quality**.
  The human sets those bounds up front and intervenes only when the system asks.
  The bounds are *fluid*: the agent works to the spec, and when the spec proves
  contradictory, infeasible, or outdated, it escalates to *renegotiate* the bound
  rather than break it. The human is a supervisor who owns the bounds, not an
  operator who owns the keystrokes.

The bar that matters is not "did it compile once." It is **correct,
self-contained, maintainable, and efficient enough to build on and maintain for
years.** BASE is the claim that an agent can clear that bar repeatedly, within
human-set, fluid bounds, with limited oversight, *if the surrounding system is
built for it.*

---

## 2. The bottleneck is the scaffolding, not the model

Hand a capable model a large codebase and the naive instruction "improve it," and
it degrades — not because it is unintelligent, but because the *environment* fails
it in predictable ways:

- **Context loss.** What memory survives between sessions is limited, and much of
  it is *re-derived by inspecting the very project being built* — so it captures
  the codebase's current drift and builds on it rather than anchoring to the
  original intent. The agent re-derives (and re-breaks) what it already knew.
- **Drift.** With no human-owned design spec to anchor to, agents infer intent
  from the existing code and docs — then invent new design parameters and build on
  them. Each pass drifts further from the original objectives, and there is
  nothing to pull it back.
- **No quality floor.** With no mechanical gate, quality is whatever the agent
  thinks it should be — judged against the project's existing quality, or the
  training-average it defaults to. The bar ratchets *downward* each pass, and
  regressions slip through as "pre-existing, not mine."
- **No coordination.** Agents work independently, each seeing only its own slice —
  not the whole, and not the environments it can't reach. With no coordinator to
  oversee the system and sequence their sessions, agents collide, duplicate, or
  undo each other, and work that must fit together drifts apart.

Every one of these is an *engineering* problem with an *engineering* answer.
Autonomy is not unlocked by waiting for the next model; it comes from building
the system that lets the current model work unattended without drifting. The model
is the engine. BASE is the rest of the car around it.

A second, quieter lever sits underneath: **the language the agent builds in.**
BASE works with any language — but the language can help or fight it. Mainstream
languages were designed for humans reading in an IDE with full project context —
hidden effects, implicit behavior, and many ways to do one thing, all of
which make an agent's output hard to verify and maintain. A language where nothing
is hidden, everything is explicit, and there is one obvious way to do each thing
makes BASE more effective and the software faster to build. (That is the thesis of
Promise, and the reason the worked example in §5 is self-referential.)

---

## 3. An operating system for bounded autonomy

BASE has seven subsystems. Each maps to a concrete mechanism in Reactor; together
they form the scaffolding the model runs inside.

### Define — durable intent

Humans own *what* and *why*; agents own *how*. The "what/why" is captured as
durable, reviewable artifacts — **design-decision documents** that fix the
high-level calls, and **work items** (in Reactor: GitHub issues, the single source
of truth) that define each unit of work. Intent lives in version control, not in a
chat that scrolls away. An agent resolving an item reads the same definition every
time; the design docs are the constitution it builds to.

The human owns the *what*, but not every work item is human-written. Most are
agent-filed — derived from the original intent and the context that surfaces while
implementing it — so the backlog fans out from the human's design without the
human authoring each line. The discipline is what keeps that fan-out anchored:
when an item is resolved, anything that does not work as expected is either fixed
inline or filed as a new work item traceable to the *what* (directly or through a
design doc), **never worked around.**

The contract itself is not static — it evolves as work surfaces contradictions or
new needs, whether an agent or a human raises the change. But every revision is an
*explicit, reviewable edit* to the artifact, never a silent drift. That amendment
path is the *Engage* subsystem below.

### Guide — a mechanical definition of correct

Quality that depends on reviewer goodwill does not survive unattended work. BASE
makes quality *mechanical*: **gates** that emit metrics, and **baselines** that
ratchet so the metrics can only improve. In Reactor, a project *declares* its gates
(`bin/gate list --json`) and Reactor *schedules* them; each gate emits a structured
envelope with a flat metric map. Metrics carry a direction and an enforcement mode
— `test_count` only goes up, `test_failures` and `leak_count` stay at zero, all
`enforced`, with caps that stop a fluke from poisoning the baseline. A regression
fails the gate, automatically, on every supported platform. This is what makes
unattended work *trustworthy*: the floor cannot silently drop.

### Resolve — the autonomous loop

The unit of autonomy is the **flow** — a self-describing binary that claims one
eligible item, resolves it, and produces a result: a PR, or (on the production
line) a merged change behind gates. `bin/flow resolve` does it once;
`bin/flow auto` runs it in a loop until a stop condition — quota, cost cap, or an
empty queue. The loop, not the single shot, is the point: it is how a
backlog drains without a human pressing the button each time.

### Orchestrate — state across a fleet

Resolution at scale needs a backlog, leases so two runners do not collide, a
scheduler that resolves items in a conflict-avoiding order, and run history that
persists. This is Reactor proper. **Stable identity** is the spine — every item,
gate, and run has a durable id (GitHub issue/PR numbers, with a private overlay
keyed to them), so the system has memory the agents lack. Orchestration is the
kernel: it schedules the flows, runs the gates, holds the ledger, and keeps the
whole production line coherent across many concurrent runs.

### Scale — hosts and arenas

Autonomy is only as fast as the substrate it runs on. Reactor manages a farm of
**arenas** — execution hosts, whether permanent machines or ephemeral cloud
instances — and routes work to them by capability (a gate that needs `linux/arm64`,
a cross-platform check that needs three OSes a single human cannot run). The farm
is the parallelism substrate, and it is where the open question of §6 lives: *how
far does autonomous construction throughput scale before it hits the
serial-dependency ceiling?*

### Endure — survive the long run

Unattended for days, the loop *will* hit failure — so resilience is its own
subsystem, not an afterthought. The hazards fall into three classes: **external
outages** (AI quota exhausted, the model provider or GitHub down, the network
gone), **infrastructure loss** (a host that reboots on a power cut or auto-update,
an arena gone unreachable, a disk that fills), and **runaway execution** (a gate
that leaks memory or overflows the stack, a fork storm, a run that stalls forever,
a prompt that loops on the same move). A system meant to run for weeks has to
absorb all of them with no one watching.

It does so on the **stable identity** spine: run state, leases, and artifacts are
persisted against durable ids through atomic, retried writes, so an interrupted run
is *resumed, not restarted* — completed steps, and the tokens they burned, are
never redone. A dead runner's lease is reclaimed; **stop conditions** turn quota
and cost exhaustion into a graceful pause rather than a crash; timeouts bound
stalls and reconciliation heals drift; and because arenas are disposable, a fouled
one is rebuilt from clean. Hardening against the full menagerie of failure modes is
ongoing — but progress, once made, is meant to stick.

### Engage — the human, by design

This is the defining inversion — and where bounded autonomy keeps its bounds
*fluid*: the governing principle is **autonomy by default with deliberate
escalation**, *not* "by exception." The human is **off the critical path**; the
system decides on its own and routes a call to the human
only when it judges that the call *should rise* to intervention — an ambiguous
design decision, a gate that needs judgment, a PR to review. That makes the
interface **deliberate**, not a fallback — "by exception" fairly names only the
narrow subset the system genuinely *cannot* resolve (a design that holds a
contradiction, a step that proves infeasible, a change to intent surfaced
mid-build), never the overall framing. Most engagement is **asynchronous and
capacity-bounded**, kept *off the critical path when it can be* — the loop runs
while a decision is pending.
Some is **optional with fallback**: if no guidance arrives, the system decides for
itself rather than let the item stall and fall out of sync. But some decisions are
broad enough that *no work can proceed* until the human makes them — there,
engagement **blocks the whole line** by necessity.

Today that surface is an **inbox** — a queue of notifications the supervisor works
through. The direction is to evolve it into a **feed**: a social-media-style stream
the human engages with on their own schedule, deciding what to attend to, while the
resolution loop keeps running underneath. Human attention is the *scarcest
resource*, and the system is engineered to spend as little of it as possible —
minimizing engagement and routing around it where it can, never manufacturing a
wait it could avoid.

---

## 4. Trust roles

The spec of §1 bounds *whether the work is right* (intent + quality, owned by the
human through *Define* and *Guide*). A second, orthogonal bound governs *who may
act* — a **bound on authority**: who may admit an item into resolution, and who
may land its result. A correct result from an untrusted hand still must not reach
origin on that hand's say-so.

Reactor draws the authority bound with **trust roles**. Resolution is not one
atomic act by one actor; it decomposes into steps assigned to roles at
**graduated trust**. Untrusted work — a contributor's, or any untrusted agent
run — is *bracketed* by trusted-agent gates it cannot cross on its own:

- **Intake** *(trusted, front gate — a generalization beyond today's
  architecture)* — a trusted session would validate a filed item and mark it
  **approved for resolution**, or bounce it, or escalate. *Admission* is an
  authority the filer does not hold.
- **Resolution** *(untrusted; `flow:resolve`)* — the less-trusted role runs
  *every* flow step — read intent, implement, test, gate locally — *except one*:
  it cannot push to origin. It produces a candidate, a **PR**, never a landing.
- **Review** *(trusted, back gate; `flow:review`/`flow:gate`)* — a trusted
  session gates the candidate before it reaches origin: **merge** it, or
  **return-to-sender** (item back to open with notes) — and, where the call
  should rise to a person, **escalate** it, the *Engage* surface of §3.

The **human sits atop the trust ladder**, reached only on escalation — the same
deliberate hand-off used to renegotiate the correctness bound. Because **PRs are
first-class items with their own identity**, a review binds to *that* candidate
and never bleeds onto another. The same engine runs at any scale — a cloud
production line draining a backlog across the arena farm, or a local `bin/flow auto`
mini-line a single contributor self-caps; what changes is throughput, not the
trust model.

The resolve-then-PR boundary, trusted review, and return-to-sender are in the
architecture today; the front **intake** gate — and review's escalate-to-human
verdict — generalize the same shape to the moment of admission and to the §3
hand-off. *PR intake is one instance of this pattern, not the whole of it.* The
shape is the point: *untrusted work, bracketed by trusted gates, with the human
at the top of the ladder.*

---

## 5. The worked example: Promise

The strongest evidence for BASE is not a benchmark — it is an artifact built under
it. **Promise** is a statically-typed, AOT-compiled systems language with a
multi-module standard library and a four-target toolchain (Linux, macOS, Windows,
WASM). Its compiler, stdlib, and catalog are written by AI agents; humans direct
the high-level design through design-decision docs. It is held to its bar by exactly the
machinery above: a work tracker, a multi-class gate system, a zero-memory-leak
policy, and **15,000+ tests that gate every commit and stay green across all four
targets** — ratcheted so the count only climbs and failures stay at zero.

Two facts make it a clean existence proof:

- **Scale and economics (proven).** A statically-typed compiler with ownership
  analysis and an LLVM backend is historically a team-years effort. This one was
  built by agents under one person's direction, on a **single $200/month
  subscription**, on hardware already on hand. (The precise time-to-a-stable
  milestone is still being measured — see §6 — but the cost structure is a
  flat-rate subscription, not a team.)
- **Self-reference.** Promise is a language designed so agents write maintainable
  code, *built by agents* — and the methodology that built it (BASE) is what Reactor
  generalizes. The language is part of the scaffolding (§2) and the artifact the
  scaffolding produced.

The next layer of evidence is the **[zoo](https://github.com/promise-language/zoo)**
— a public gallery of real programs built *in* Promise by agents. That is where
the *quality* question gets tested in the open (§6).

---

## 6. What's proven, what's in progress, what's open

Credibility depends on not overclaiming. The state of the evidence, honestly:

- **Proven — scale + economics.** Agents built a large, complex, long-lived
  systems project (the Promise platform) on a $200/month subscription. The
  artifact exists and is maintained this way. *This answers "can agents build
  something big and real at all?" — yes.*
- **In progress — quality.** Can an agent take a *genuinely complex* problem and
  build a correct, maintainable solution *on* the platform, with limited
  oversight? The zoo is the instrument. It is thin today; the first non-trivial
  attempt shipped and ran correctly but only after the agent fought real platform
  friction. The signal to watch is **convergence** — each hard target surfacing a
  bounded, shrinking set of issues rather than an endless stream. *This is not yet
  proven, and we do not claim it is.*
- **Open — how far does throughput scale?** Mechanical parallelism is *proven*:
  up to 8 items have run concurrently across different arenas, sometimes on
  different hosts — there is no "one item at a time" rule anywhere. Usual
  concurrency is ~1 for a purely economic reason: a single resolution already
  outspends the $200/month subscription, so running more only outpaces quota and
  forces a wait. Even then concurrency is real — items **park** (awaiting a human
  answer, or a decision to grant more resources) while another proceeds, so
  multiple builds are genuinely in-flight. What is *open* is whether construction
  **throughput** scales near-linearly given more budget/agents/substrate, or hits
  a **serial-dependency ceiling** — you cannot build on the typechecker before it
  works; a fix can gate the next task. That is unknown, and the arena farm (§3) is
  the instrument built to measure it.
- **Future — external validation.** Independent review and third-party benchmarks
  are the eventual proof that quality is not self-certified. That requires more
  platform stability and coverage first; it is not yet possible, and we flag it as
  future rather than imply it.

---

## 7. Where this goes

- **Engagement: inbox → feed.** Evolve the §3 inbox into a feed the supervisor
  pulls from on their own schedule — driving the human-attention cost of the
  resolution loop toward its floor.
- **Parallelism.** Push the arena farm until the serial ceiling shows itself, and
  report where it is — the answer is itself a research result about BASE.
- **Multi-project.** Reactor's project-owns-domain / orchestrator-stays-thin split
  is built so the same engine drives more than Promise; BASE is a methodology, not
  a single codebase.
- **The ecosystem.** Promise (the language and proof), **flow** (the resolution
  layer), **forge** (the dev-tooling blueprint), and **Reactor** (orchestration)
  are siblings that go public together.

---

## 8. The bet, restated

Bounded-Autonomy Software Engineering is a falsifiable bet: that large, complex,
maintainable software can be built and kept alive by agents working under human
design direction — the system autonomous by default, the human off the critical
path and engaged only by deliberate escalation. The claim is that what stands
between today and that future is *building the system around the model*, not
waiting for a better model.

The existence proof is on the table. The quality proof is being run in the open.
The scaling question is honestly unanswered.

Reactor is the open-source bet that this discipline is reproducible — not one
project's story, but a system anyone can run. It is early yet: a design, not a
finished tool. The wager is that what worked for Promise will work for any project
run this way — not just the one that proved it.
