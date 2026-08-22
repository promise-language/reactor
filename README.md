# Reactor

**Reactor is the open-source orchestration system for Bounded-Autonomy Software
Engineering (BASE)** — the discipline of building and maintaining large, complex
software with AI agents that are free over the *how* but held to a human-owned
design specification.

Reactor manages a backlog of work items across a fleet of execution **arenas**,
runs **flows** (self-describing agent binaries) to resolve them, and enforces
continuous quality with mechanical **gates** — running autonomously by default
and escalating to a human by design. It succeeds a private predecessor,
`tracker`, as a clean-sheet implementation rather than a port.

- **[Read the white paper →](WHITEPAPER.md)** — the BASE methodology and the bet behind it.
- **[Architecture →](docs/design.md)** — the authority model, deployment topology, and the pluggable persistence split.
- **[The BASE layer →](docs/base-engineering.md)** — flows, gates, the discovery contract, and what a project owns versus what is delivered to it.
- **[The engagement feed →](docs/engagement-feed.md)** — the single surface the system calls a human to, ranked by regret per minute of attention.
- **[Promise dev tooling →](docs/dev-tooling.md)** — how project tools are built and run once they are written in Promise.

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

Reactor is the **orchestrator** — thin by design, and reusable across projects
rather than built around any one of them. Domain logic is pushed out of it along
a single line: **a gate measures the tree, so it comes from the tree** (the
project owns and builds it), while **a flow modifies the tree, so it comes from
outside the tree** (project-specific, but versioned elsewhere so fixing one never
collides with work already in flight). Reactor builds neither — it distributes
flows that each project's CI publishes:

- **Durable intent.** Work items are GitHub issues — the single source of truth —
  and design-decision docs in version control, not a chat that scrolls away.
- **An autonomous loop.** A *flow* is a self-describing binary that resolves one
  item — a step at a time — and produces a PR, or a gated, merged change. It is
  handed its work rather than taking it: the runner polls, Reactor decides what
  is eligible, and the loop runs until a stop condition — quota, cost cap, or an
  empty queue.
- **Bounded authority.** Every actor has a role, and every step of every flow
  declares what it may touch. Effective authority is the intersection, enforced
  outside the flow — a `plan` step cannot edit source even when an admin runs it.
- **Mechanical quality.** Projects *declare* their gates; Reactor *schedules*
  them. Metrics ratchet — test counts only climb, failures and leaks stay at zero
  — so the floor cannot silently drop, on every supported platform.
- **State across a fleet.** A backlog, leases so two runners don't collide, a
  conflict-avoiding scheduler, and run history persisted against stable ids — the
  memory the agents lack.
- **An arena farm.** Work routes to execution hosts (permanent machines or
  ephemeral cloud instances) by capability — the parallelism substrate.
- **Unattended operation.** The system is built to *keep* running with nobody
  watching — across crashes, partitions, exhausted quotas, hung agents, and
  rebooted machines. Two rules carry it: **never stall** — every wait is backed
  by a live process and a deadline, so nothing waits on what will never arrive —
  and **never spin** — an attempt that costs tokens or machine time must differ
  from the one it repeats. So interrupted runs *resume, not restart*, dead leases
  are reclaimed, and quota exhaustion pauses gracefully instead of crashing.
- **The human, by design.** Autonomy by default with *deliberate escalation* —
  the system routes a call to a person (an ambiguous design decision, a gate that
  needs judgment, a PR to review) only when it judges the call should rise,
  keeping them off the critical path. What does rise lands in one place: a ranked
  [feed](docs/engagement-feed.md) any component can post to — a stream engaged with
  on the human's own schedule, ordered by what it costs to leave each article
  undone per minute of attention it takes.

Untrusted work — a contributor's, or any untrusted run — is **bracketed by
trusted gates**: a less-trusted role runs every step *except* pushing to origin
(it produces a PR), and a trusted review either merges, returns it to sender, or
escalates to the human atop the trust ladder. That bracketing is not a convention
the flow is asked to honor — it falls out of the role and step grants, which are
declared where the agents they constrain cannot reach them. The same engine runs
at any scale, from a production line draining a backlog to a single contributor
resolving one issue under a restricted role. *(White paper §4.)*

## How it runs

Three processes, and the split is deliberate:

- **The server** is cloud-hosted. It holds all state, makes every dispatch
  decision, serves the admin UI, checks authority on every mutation, and
  distributes binaries.
- **A runner** lives in each workspace — a developer's machine, a container, an
  ephemeral cloud VM — and executes the work: flows, gates, worktree preparation.
- **A governor** supervises the runners on each host, restarting them and swapping
  in updates. Sandboxes are supervised instead by the runner that created them.

**The server never reaches into a host.** Runners always open the connection
themselves and poll for work, so they can sit behind NAT, inside containers, and
on machines that come and go — no inbound firewall holes, no SSH credentials, no
per-host reachability to arrange.

Work runs in separate processes rather than inside the orchestrator. That is what
makes a failure isolatable, a resource limit real, and a hung agent killable —
the properties unattended operation actually depends on. Everything the runner
starts is a child process it watches and bounds by a deadline; every lock, from
an item claim to "an integration is in progress", is a lease held by a named
process and released automatically when that process dies.

## Status

**Early bootstrap — design, not yet engine.** The repo today is a Promise skeleton
under `cmd/reactor`, the Go dev tooling that builds the commit gate, the licenses,
the [white paper](WHITEPAPER.md), and the [design docs](docs/), where nearly all
the work so far has gone.

Two objectives govern the work: a **clean, reusable BASE implementation that
applies to many projects**, and **running reliably unattended for prolonged
periods**. They meet in the authority model — when nobody is watching, the only
thing between a mistake and damage is what an agent was *able* to do, which is
why the guardrails are load-bearing rather than decorative.

Settled enough to build against: the process topology, the gate and flow
contracts, the authority model down to the capability vocabulary it is expressed
in, and where each piece lives — the reusable layer is
[`base`](https://github.com/promise-language/base), one repo publishing several
independently addressable modules. A build order is unblocked but not yet drawn;
two things gate its *start* rather than its shape — `promise run` argument
passing, without which no Promise dev tool can take arguments, and the release cut
that promotes subdirectory modules out of trunk. Neither touches the design, so
the contract work runs in parallel and is waiting when they land.

Reactor is written in **[Promise](https://github.com/promise-language/promise)**,
as are the runner, the governor, and the flows — making it the platform's first
large application as well as its orchestrator. That is a real bet, not a
formality: Reactor needs TLS, DNS, crypto, and a concurrent HTTP server that
Promise does not have yet, and those gaps are tracked as platform requests rather
than worked around. A project's own gates may be written in any language, since
that boundary is a JSON contract over a subprocess — so BASE can orchestrate a
project it shares no runtime with.

The bet is falsifiable, and the evidence is staged honestly:

- **Proven — scale.** Agents built and maintain a large, complex, long-lived
  systems project — the [Promise](https://github.com/promise-language/promise)
  language, compiler, standard library, and catalog, gated by 17,703 tests
  across five targets (Linux x86-64 and arm64, macOS, Windows, WASM) as of
  August 2026 — under one person's design direction.
- **In progress — quality.** Whether agents can take a genuinely complex problem
  and build a correct, maintainable solution *on* the platform with limited
  oversight is being tested in the open, in the [zoo](https://github.com/promise-language/zoo).
- **Open — throughput.** How far autonomous construction scales before it hits a
  serial-dependency ceiling is unknown; the arena farm is the instrument built to
  measure it.

## The ecosystem

Reactor is one of several sibling repos:

- **[promise](https://github.com/promise-language/promise)** — the language and
  the existence proof: designed so agents write maintainable code, built by agents.
- **[base](https://github.com/promise-language/base)** — the shared BASE layer:
  every contract Reactor, flows, and gates speak — wire types, the gate manifest
  and envelope, flow self-description, authority config — plus the reusable
  machinery built on them, from the flow common library and gate SDK to arena
  provisioning and ratcheting baselines.
- **Reactor** — orchestration: the production line that drains a backlog across
  the arena farm.
- **[zoo](https://github.com/promise-language/zoo)** — the public gallery where
  the quality half of the bet is being tested in the open.

Each orchestrated project also has a **companion repo** holding its own BASE
setup — flow steps, item types, prompts, and authority config — kept outside the
project source so that fixing a flow never contends with work in flight, and so
an agent cannot edit the rules that bound it.

Two earlier repos are prior art rather than plan: the Go `flow` SDK and the
`forge` dev-tooling blueprint. What is reusable in them is folded into `base` —
[what lives where](docs/base-engineering.md#what-lives-where) is now settled.

## Build

Reactor's dev tooling is still Go — one in-repo module that compiles every dev
tool into `bin/`:

```sh
./make        # compile dev tools into bin/ (verify, guard, precommit, setup)
bin/verify    # the commit gate: format → build → test
```

`bin/` is gitignored; tools are built on demand, never committed.

That apparatus exists only to work around limits of the Go toolchain, and is
expected to go away rather than be ported — see
[Promise dev tooling](docs/dev-tooling.md).

Reactor itself is a Promise program. Install the toolchain from the
[latest release](https://github.com/promise-language/promise/releases/latest),
then:

```sh
promise run cmd/reactor                        # run in place
promise build -release -o bin/reactor cmd/reactor
```

Each deployable is its own Promise module under `cmd/` — `reactor` today, with
`runner` and `governor` to follow. CI builds the linux-amd64 release binary from
`main`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Contributions require signing the Promise
Lang CLA — the bot prompts you on your first PR — and are dual-licensed as below.

## License

Dual-licensed under either [Apache-2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT),
at your option. See [LICENSE](LICENSE).
