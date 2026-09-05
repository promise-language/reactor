# Documentation Index

This is the map of `docs/`. It is the one file in the root that is not a specification —
everything else there is.

**The rules are defined once, org-wide, in [org/normative.md](org/normative.md)**: which
locations bind and which do not, the tag header, where status lives and where it never does,
one fact one home, the lifecycle of a specification, and what is enforced mechanically. This
index does not restate them. Progress prose lives in the
[README's Status section](../README.md#status) and nowhere else.

**This project's status query.** Each root document's tag is a GitHub label, spelled as the
file's basename minus `.md`, and the remaining work for a document is:

> `gh issue list --label <tag> --state open --limit 200`

`--state open` and `--limit 200` are both written out deliberately: `gh issue list` defaults to
a limit of 30, so a specification with more remaining work than that would silently
under-report and read as nearly done.

## Specifications

| | Defines | Read it when |
|---|---|---|
| **[design.md](design.md)** | **What Reactor is.** The authority model, identity, the process topology and its trust boundary, persistence, reliability, scheduling, and how steps and gates are executed. | You are building Reactor, or you need to know what the orchestrator guarantees. |
| **[base-engineering.md](base-engineering.md)** | **What a project must provide, and what BASE provides to it.** The six invariants, flows, the gate contract, step resolution, and what lives in which repo. | You are adopting BASE for a project, or writing a flow or a gate. |
| **[engagement-feed.md](engagement-feed.md)** | **How the system spends human attention.** The article, ranking by regret per minute, questions and deadlines, and the authority model over what a reader can do from a card. | You are building the feed, or anything that needs to reach a human. |
| **[dev-tooling.md](dev-tooling.md)** | **How project dev tooling is built and run** once the tools are written in Promise. | You are working on `./make`, `bin/verify`, or the tool bootstrap. |

## Organization-wide corpus — binding

Vendored from [promise-language/org](https://github.com/promise-language/org) at the release
named in [org/stamp.json](org/stamp.json). Never edited here: an issue about one of these
documents is filed against `org` (org/normative.md §7); what this project files locally under
their tags is its own compliance gaps.

- [org/normative.md](org/normative.md) — What makes a document binding, and the one docs
  structure every project holds.
- [org/engineering-guide.md](org/engineering-guide.md) — How code in this organization is
  written, in any language.
- [org/engineering-guide-promise.md](org/engineering-guide-promise.md) — The engineering guide
  applied to Promise source.
- [org/engineering-guide-go.md](org/engineering-guide-go.md) — The engineering guide applied to
  Go source.
- [org/cli-guide.md](org/cli-guide.md) — How every command-line tool behaves at its invocation
  surface.
- [org/stamp.json](org/stamp.json) — The version stamp: the org release these copies came from,
  with per-file hashes.

## Archive — superseded or delivered

- [archive/engineering-guide.md](archive/engineering-guide.md) — the vendored copy of base's
  engineering guide, hand-synced while that was the mechanism. Superseded by
  [org/engineering-guide.md](org/engineering-guide.md), whose home is the org repository.

## How they fit together

**design.md and engagement-feed.md are peers** — both describe Reactor. The feed is separate because
it is large and self-contained, not because it is optional; design.md carries a summary and links
here for the schema, the same relationship the gate manifest has between the two documents below.

**base-engineering.md is the other side of a boundary.** design.md ends where the project begins:
Reactor schedules and executes, a project declares its gates and owns its flow definitions. Read
along that seam and the two documents interlock rather than overlap — the gate contract is specified
once, in base-engineering, and referenced from design.

**dev-tooling.md is scaffolding**, not architecture. It exists because the Go toolchain forced a
workaround, and it is expected to be deleted rather than maintained.

**Cross-references are load-bearing.** Where one document says "the same rule as X", that is a
claim the two mechanisms genuinely share a design, and it is meant to be checked.

**[Decisions locked](design.md#decisions-locked)** at the end of design.md is the short form of the
whole corpus — a settled statement per line. It is the fastest way to disagree with the
architecture without reading all of it.
