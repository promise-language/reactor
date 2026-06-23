<!-- Status: draft. Conceptual/methodology layer ABOVE docs/design.md (which is the
     architecture). Reactor is pre-public; promise/flow/forge/reactor go public together,
     so this doc ships when reactor opens. Keep claims at honest maturity (see §6):
     assert the existence proof, frame quality as an open experiment, never bake a
     build-duration number, quote the $200/mo tier (not a summed total). -->

# Autonomous Software Development

*A white paper on building and maintaining large, complex software with AI agents
under human direction — and on Reactor, the system that makes it work.*

---

## Abstract

The common verdict on AI-written code is that it tops out at "slop": fine for a
snippet or a prototype, but unable to build — let alone *maintain* — a large,
complex system without a human in the loop cleaning up after every step. This
paper argues the ceiling is real but misattributed. It is not a ceiling on the
*model*; it is a ceiling on the *system around the model*. Give an agent durable
intent, a mechanical definition of "correct," an automated resolution loop, an
orchestrator that holds state across a fleet, and a human who is engaged *by
design*, autonomous by default and escalated to deliberately, kept mostly off the
critical path rather than strapped into it — and the work that survives is large,
complex, and maintainable.

We call that discipline **Autonomous Software Development (ASD)**, and we make a
falsifiable claim about it: ASD is an *engineering* problem, solvable with
scaffolding, not a *capability* problem waiting on a better model. The existence
proof is concrete — the [Promise language](https://github.com/promise-language/promise)
compiler, standard library, and catalog were built and are maintained this way,
by agents under human design direction, on a single $200/month subscription.
**Reactor** is the open-source orchestration system that runs the loop. This
paper defines the methodology; [`docs/design.md`](docs/design.md) defines
Reactor's architecture.

---

## 1. Autonomous, not assisted

ASD is not a coding assistant, and it is not "vibe coding." The distinction is
not the model or the prompt — it is **where the human sits relative to the build
loop.**

- **Assisted development** (copilots, chat-in-the-IDE) keeps the human *in* the
  loop: the agent proposes, the human accepts, edits, and drives. Throughput is
  bounded by human attention, one keystroke at a time.
- **"Vibe coding"** removes the human but also removes the bar: generate
  something that runs once, ship it, hope. It produces exactly the slop the
  critics describe.
- **Autonomous development** removes the human *from the moment-to-moment loop* while
  *raising* the bar: the agent defines, implements, tests, and gates the work; the
  human sets intent up front and intervenes only when the system asks. The human
  is a supervisor, not an operator.

The bar that matters is not "did it compile once." It is **correct,
self-contained, maintainable, and efficient enough to build on and keep
maintaining for years.** ASD is the claim that an agent can clear that bar
repeatedly, with limited oversight, *if the surrounding system is built for it.*

---

## 2. The bottleneck is the scaffolding, not the model

Hand a capable model a large codebase and the naive instruction "improve it," and
it degrades — not because it is unintelligent, but because the *environment* fails
it in predictable ways:

- **Context loss.** No memory survives between sessions; the agent re-derives
  (and re-breaks) what it already knew.
- **Drift.** Without a fixed definition of "done," each change pulls the codebase
  toward a slightly different dialect, and the whole accretes inconsistency.
- **No quality floor.** Nothing mechanically prevents a regression, a leak, or a
  silent correctness loss; "looks right" is the only gate.
- **No coordination.** Two agents (or two runs) collide, duplicate, or undo each
  other; there is no shared backlog, no leases, no source of truth.

Every one of these is an *engineering* problem with an *engineering* answer.
Autonomy is not unlocked by waiting for the next model; it is unlocked by building
the system that lets the current model work unattended without drifting. The model
is the engine. ASD is the rest of the car.

A second, quieter lever sits underneath: **the language the agent builds in.**
Mainstream languages were designed for humans reading in an IDE with full project
context — full of hidden effects, implicit behavior, and many ways to do one
thing, all of which make an agent's output hard to verify and maintain. A language
where nothing is hidden, everything is explicit, and there is one obvious way to
do each thing is itself part of the scaffolding. (That is the thesis of Promise,
and the reason the worked example in §5 is self-referential.)

---

## 3. An operating system for autonomous development

ASD has six subsystems. Each maps to a concrete mechanism in Reactor; together
they are the operating system around the model.

### Define — durable intent

Humans own *what* and *why*; agents own *how*. The "what/why" is captured as
durable, reviewable artifacts — **design-decision documents** that fix the
high-level calls, and **work items** (in Reactor: GitHub issues, the single source
of truth) that define each unit of work. Intent lives in version control, not in a
chat that scrolls away. An agent resolving an item reads the same definition every
time; the design docs are the constitution it implements against.

### Guide — a mechanical definition of correct

Quality that depends on reviewer goodwill does not survive unattended work. ASD
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
eligible item, resolves it, and produces a result — a PR, or, on the production
line, a merged change behind gates. `bin/flow resolve` does it once;
`bin/flow auto` runs it in a loop until a stop condition — quota, cost cap, or no
eligible items remain. The loop, not the single shot, is the point: it is how a
backlog drains without a human pressing the button each time.

### Orchestrate — state across a fleet

Resolution at scale needs a backlog, leases so two runners do not collide, a
scheduler that resolves items in a conflict-avoiding order, and run history that
persists. This is
Reactor proper. **Stable identity** is the spine — every item, gate, and run has a
durable id (GitHub issue/PR numbers, with a private overlay keyed to them), so the
system has memory the agents lack. Orchestration is the kernel: it schedules the
flows, runs the gates, holds the ledger, and keeps the whole production line
coherent across many concurrent runs.

### Scale — hosts and arenas

Autonomy is only as fast as the substrate it runs on. Reactor manages a farm of
**arenas** — permanent hosts and ephemeral cloud arenas — and routes work to them
by capability (a gate that needs `linux/arm64`, a cross-platform check that needs
three OSes a single human cannot run). The farm is the parallelism
substrate, and it is where the open question of §6 lives: *how far does
autonomous construction throughput scale before the serial-dependency floor?*

### Engage — the human, by design

This is the defining inversion, and the governing principle is **autonomy by
default with deliberate escalation** — *not* "by exception." The human is **out of
the critical loop**; the system decides on its own and routes a call to the human
only when it judges that the call *should rise* to intervention — an ambiguous
design decision, a gate that needs judgment, a PR to review. That makes the
interface **intentional, by design**. ("By exception" fairly names only the narrow
subset the system genuinely *cannot* resolve — a design that holds a contradiction,
a step that proves infeasible, a change to intent surfaced mid-build — never the
overall framing.) Most engagement is **asynchronous and capacity-bounded**, kept
*off the critical path when it can be* — the loop runs while a decision is pending.
Some is **optional with fallback**: if no guidance arrives, the system decides for
itself rather than let the item stall and drift out of sync. But some decisions are
broad enough that *no work can proceed* until the human makes them — there,
engagement **blocks the whole line** by necessity.

Today that surface is an **inbox** — a queue of notifications the supervisor works
through. The direction is to evolve it into a **feed**: a social-media-style stream
the human engages with on their own schedule, deciding what to attend to, while the
development loop keeps running underneath. Human attention is the *scarcest
resource*, and the system is engineered to spend as little of it as possible —
minimizing engagement and routing around it where it can, never manufacturing a
wait it could avoid, and blocking only on the broad decisions no work can proceed
past.

> **Design tenets.** Push domain logic to the project; keep the orchestrator thin.
> Quality is mechanical, not social. Humans own intent; agents own implementation.
> One identity authority (GitHub) so nothing leaks or drifts. Engage the human by
> design — autonomous by default, escalated deliberately, blocking only when a
> decision is too broad to route around.

---

## 4. The four engagement modes

Deliberate engagement is not one-size-fits-all; Reactor supports a
gradient of human involvement (detailed in [`docs/design.md`](docs/design.md)):

1. **Production line** — a human drives a large backlog across the arena farm; the
   cloud server resolves items in a conflict-avoiding order with continuous gates.
   Lowest human-per-item engagement.
2. **Manual resolution** — clone the project, `bin/flow resolve` one issue, open
   a PR. No server; gates run locally. Highest engagement.
3. **Unattended loop** — `bin/flow auto`, the loop that runs until a stop
   condition, self-capped by the human's own quota.
4. **PR intake** — review and security flows plus cross-platform gates on
   ephemeral arenas decide merge or return-to-sender.

The same engine spans solo-unattended to multi-human-governed; what changes is how
often, and on what, the human is asked to engage.

---

## 5. The worked example: Promise

The strongest evidence for ASD is not a benchmark — it is an artifact built under
it. **Promise** is a statically-typed, AOT-compiled systems language with a
multi-module standard library and a four-target toolchain (Linux, macOS, Windows,
WASM). Its compiler, stdlib, and catalog are written by AI agents; humans direct
the high-level design through decision docs. It is held to its bar by exactly the
machinery above: a work tracker, a multi-class gate system, a zero-memory-leak
policy, and **13,000+ tests that gate every commit and stay green across all four
targets** — ratcheted so the count only climbs and failures stay at zero.

Two facts make it a clean proof:

- **Scale and economics (proven).** A statically-typed compiler with ownership
  analysis and an LLVM backend is historically a team-years effort. This one was
  built by agents under one person's direction, on a **single $200/month
  subscription**, on hardware already on hand. (The precise time-to-a-stable
  milestone is still being measured — see §6 — but the cost structure is a
  flat-rate subscription, not a team.)
- **Self-reference.** Promise is a language designed so agents write maintainable
  code, *built by agents* — and the methodology that built it (ASD) is what Reactor
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
- **Open — how far does throughput scale?** Mechanical parallelism is *proven*: up to 8 items have run concurrently across different arenas, sometimes on different hosts — there is no "one item at a time" rule anywhere. Usual concurrency is ~1 for a purely economic reason: a single resolution already outspends the $200/month subscription, so running more only outpaces quota and forces a wait. Even then concurrency is real — items **park** (awaiting a human answer, or a decision to grant more resources) while another proceeds, so multiple builds are genuinely in-flight. What is *open* is whether construction **throughput** scales near-linearly given more budget/agents/substrate, or hits a **serial-dependency floor** — you cannot build on the typechecker before it works; a fix can gate the next task. That is unknown, and the arena farm (§3) is the instrument built to measure it.
- **Future — external validation.** Independent review and third-party benchmarks
  are the eventual proof that quality is not self-certified. That requires more
  platform stability and coverage first; it is not yet possible, and we flag it as
  future rather than imply it.

---

## 7. Where this goes

- **Engagement: inbox → feed.** Move from a notification queue to a stream the
  supervisor engages with on their own schedule, minimizing the human attention
  the loop consumes.
- **Parallelism.** Push the arena farm until the serial ceiling shows itself, and
  report where it is — the answer is itself a research result about ASD.
- **Multi-project.** Reactor's project-owns-domain / orchestrator-stays-thin split
  is built so the same engine drives more than Promise; ASD is a methodology, not
  a single codebase.
- **The ecosystem.** Promise (the language and proof), **flow** (the resolution
  layer), **forge** (the dev-tooling blueprint), and **Reactor** (orchestration)
  are siblings that go public together.

---

## 8. The bet, restated

Autonomous Software Development is a falsifiable bet: that large, complex,
maintainable software can be built and kept alive by agents under human design
direction, with the human autonomous by default and engaged only by deliberate
escalation — and that the thing standing between today and that future is
*engineering the system around the model*, not
waiting for a better model. The existence proof is on the table. The quality proof
is being run in the open. The scaling question is honestly unanswered.

Reactor is the open-source bet that this is reproducible — not a story about one
project, but a system anyone can run.
