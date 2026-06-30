# Reactor

**Reactor is the open-source orchestration system for Bounded-Autonomy Software
Engineering (BASE)** — the discipline of building and maintaining large, complex
software with AI agents that are free over the *how* but held to a human-owned
design specification.

Reactor manages a backlog of work items across a fleet of execution **arenas**,
runs **flows** (self-describing agent binaries) to resolve them, and enforces
continuous quality with mechanical **gates** — running autonomously by default
and escalating to a human by design. It is the successor to the private
`tracker` project.

- **[Read the white paper →](WHITEPAPER.md)** — the BASE methodology and the bet behind it.
- **[Architecture & roadmap →](docs/design.md)** — operating scenarios, the pluggable persistence split, milestones M0–M7.

## The idea

The common verdict on AI-written code is that it tops out at "slop." The white
paper argues the ceiling is real but misattributed: it is not a ceiling on the
*model*, it is a ceiling on the *system around the model*. Give an agent durable
intent, a mechanical definition of "correct," an automated resolution loop, an
orchestrator that holds state across a fleet, and a human engaged *by design* —
and the work that survives is large, complex, and maintainable.

**Bounded autonomy** is the discipline: the agent has genuine autonomy over the
*how*, bounded by two things the human owns — durable **intent** (the what and
why) and a mechanical definition of **quality** (gates and ratcheting baselines).
Together those are the design specification the software must satisfy, and the
bound the agent works to. Reactor is the system that runs that loop.

## What Reactor provides

Reactor is the **orchestrator** — thin by design, with domain logic pushed out to
the project (flows and gates live in the project; Reactor provides the SDKs and
holds the production line together):

- **Durable intent.** Work items are GitHub issues — the single source of truth —
  and design-decision docs in version control, not a chat that scrolls away.
- **An autonomous loop.** A *flow* is a self-describing binary that claims one
  eligible item, resolves it, and produces a PR (or a gated, merged change).
  `bin/flow resolve` does it once; `bin/flow auto` loops until a stop condition —
  quota, cost cap, or an empty queue.
- **Mechanical quality.** Projects *declare* their gates; Reactor *schedules*
  them. Metrics ratchet — test counts only climb, failures and leaks stay at zero
  — so the floor cannot silently drop, on every supported platform.
- **State across a fleet.** A backlog, leases so two runners don't collide, a
  conflict-avoiding scheduler, and run history persisted against stable ids — the
  memory the agents lack.
- **An arena farm.** Work routes to execution hosts (permanent machines or
  ephemeral cloud instances) by capability — the parallelism substrate.
- **Resilience.** Interrupted runs *resume, not restart*; dead leases are
  reclaimed; stop conditions turn quota exhaustion into a graceful pause rather
  than a crash.
- **The human, by design.** Autonomy by default with *deliberate escalation* —
  the system routes a call to a person (an ambiguous design decision, a gate that
  needs judgment, a PR to review) only when it judges the call should rise,
  keeping them off the critical path.

Untrusted work — a contributor's, or any untrusted run — is **bracketed by
trusted gates**: a less-trusted role runs every step *except* pushing to origin
(it produces a PR), and a trusted review either merges, returns it to sender, or
escalates to the human atop the trust ladder. The same engine runs at any scale,
from a cloud production line draining a backlog to a local `bin/flow auto`
mini-line a single contributor self-caps. *(White paper §4.)*

## Status

**Early bootstrap (M0).** The repo today is the [forge](https://github.com/promise-language/forge)
tooling blueprint, the licenses, the [white paper](WHITEPAPER.md), and the
[design doc](docs/design.md) with its M0–M7 roadmap — the engine itself (lifted
from `tracker`) lands in **M2**.

The bet is falsifiable, and the evidence is staged honestly:

- **Proven — scale.** Agents built and maintain a large, complex, long-lived
  systems project — the [Promise](https://github.com/promise-language/promise)
  language, compiler, standard library, and catalog, gated by 13,000+ tests
  across four targets — under one person's design direction.
- **In progress — quality.** Whether agents can take a genuinely complex problem
  and build a correct, maintainable solution *on* the platform with limited
  oversight is being tested in the open, in the [zoo](https://github.com/promise-language/zoo).
- **Open — throughput.** How far autonomous construction scales before it hits a
  serial-dependency ceiling is unknown; the arena farm is the instrument built to
  measure it.

## The ecosystem

Reactor is one of four siblings that go public together:

- **[promise](https://github.com/promise-language/promise)** — the language and
  the existence proof: designed so agents write maintainable code, built by agents.
- **[flow](https://github.com/promise-language/flow)** — the resolution layer:
  the SDK for self-describing flow binaries.
- **[forge](https://github.com/promise-language/forge)** — the dev-tooling
  blueprint Reactor builds on.
- **Reactor** — orchestration: the production line that drains a backlog across
  the arena farm.

## Build

Reactor uses the forge dev-tooling blueprint — one in-repo Go module that
compiles every dev tool into `bin/`:

```sh
./make        # compile dev tools into bin/ (verify, guard, precommit, setup)
bin/verify    # the commit gate: format → vet → build → test
```

`bin/` is gitignored; tools are built on demand, never committed.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Contributions require signing the Promise
Lang CLA — the bot prompts you on your first PR — and are dual-licensed as below.

## License

Dual-licensed under either [Apache-2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT),
at your option. See [LICENSE](LICENSE).
