# Reactor design documents

These define **what Reactor is** — the architecture the implementation must satisfy. They are not a
plan, a status report, or a record of progress. Where something is genuinely undecided, each
document says so in its own **Open questions** section; everything else in the body is a statement
about the system, not a proposal awaiting approval.

Progress lives in exactly one place: the [README's Status section](../README.md#status). If you want
to know what is built, look there, not here.

## The documents

| | Defines | Read it when |
|---|---|---|
| **[design.md](design.md)** | **What Reactor is.** The authority model, identity, the process topology and its trust boundary, persistence, reliability, scheduling, and how steps and gates are executed. | You are building Reactor, or you need to know what the orchestrator guarantees. |
| **[base-engineering.md](base-engineering.md)** | **What a project must provide, and what BASE provides to it.** The six invariants, flows, the gate contract, step resolution, and what lives in which repo. | You are adopting BASE for a project, or writing a flow or a gate. |
| **[engagement-feed.md](engagement-feed.md)** | **How the system spends human attention.** The article, ranking by regret per minute, questions and deadlines, and the authority model over what a reader can do from a card. | You are building the feed, or anything that needs to reach a human. |
| **[engineering-guide.md](engineering-guide.md)** | **How code in this repository is written** — naming, shape, testing, visibility, and what to do when the platform is in the way. Vendored from [`base`](https://github.com/promise-language/base/blob/main/docs/engineering-guide.md), which is the source. | You are writing or reviewing any Promise code here, starting with the wire schemas. |
| **[dev-tooling.md](dev-tooling.md)** | **How project dev tooling is built and run** once the tools are written in Promise. | You are working on `./make`, `bin/verify`, or the tool bootstrap. |

## How they fit together

**design.md and engagement-feed.md are peers** — both describe Reactor. The feed is separate because
it is large and self-contained, not because it is optional; design.md carries a summary and links
here for the schema, the same relationship the gate manifest has between the two documents below.

**base-engineering.md is the other side of a boundary.** design.md ends where the project begins:
Reactor schedules and executes, a project declares its gates and owns its flow definitions. Read
along that seam and the two documents interlock rather than overlap — the gate contract is specified
once, in base-engineering, and referenced from design.

**engineering-guide.md governs code rather than architecture.** It is the only document here that
constrains *how* something is written rather than *what* it must do, and it is deliberately in this
tree rather than referenced: a step resolves inside a materialized worktree of this repository and
nothing else, so a rule kept elsewhere is not in its context when it matters.

**dev-tooling.md is scaffolding**, not architecture. It exists because the Go toolchain forced a
workaround, and it is expected to be deleted rather than maintained.

## Conventions

- **A rule stated as a blockquote is an invariant**, and the prose under it is why. If the rule and
  the reasoning ever disagree, the rule is what the implementation must satisfy.
- **Cross-references are load-bearing.** Where one document says "the same rule as X", that is a
  claim the two mechanisms genuinely share a design, and it is meant to be checked.
- **[Decisions locked](design.md#decisions-locked)** at the end of design.md is the short form of the
  whole corpus — a settled statement per line. It is the fastest way to disagree with the
  architecture without reading all of it.
