# Engineering guide

> **This document defines how Promise code under BASE is written** — naming, shape, testing,
> visibility, and what to do when the platform is in the way. It governs the modules published here,
> and it is **the source every other BASE repository vendors from**; a project's own gates may be
> written in anything and are governed by their own project.
>
> **It assumes** Promise's own
> [`docs/code-style.md`](https://github.com/promise-language/promise/blob/main/docs/code-style.md)
> and `CLAUDE.md`, from which the language-level half is derived.
>
> **Enforcement is stated per rule and is mostly not built yet.** A rule with no check behind it is
> marked *advisory*, deliberately — see [Enforcement](#enforcement).

## Why this is here rather than referenced

A step resolves its item inside a materialized worktree of **this** repository and nothing else. A
rule that lives in another repo is not in the agent's context at the moment it has to be followed,
so referencing it is the same as not having it.

That is the argument the corpus already makes for gates — [a gate measures the tree, so it comes
from the tree](https://github.com/promise-language/reactor/blob/main/docs/base-engineering.md#the-principle) — with the same shape applied to guidance: it is
read in the tree, so it lives in the tree.

**So the language-level half is vendored, not linked**, which the design permits explicitly: bytes
in the tree are the project's whatever their origin. Two honest consequences:

- **It is derived, not byte-copied.** Promise's text is written for a compiler — it references
  `modules/std/*.pr`, codegen tests, IR. What is general is carried over; what is specific is
  restated. So nothing can diff this against it, and reconciling the two is a periodic human act.
- **Every BASE repository holds a copy, and this one is the source.** A repository that vendors
  this guide names it as the origin and adds only what is true of itself. When two copies disagree,
  this is the one that is right — which is the only thing keeping several copies from becoming
  several opinions.

## Naming

- **Full English words.** `print_line`, not `println`. `execute`, not `exec`.
- **Approved abbreviations are mandatory where one exists** — the dictionary is §9.3a of Promise's
  `docs/language-design.md` (`dir`, `env`, `id`, `arg`, `len`, `min`, `max`, `config`, `src`, and
  the rest). Where a mapping exists, the abbreviation is the correct form, not a tolerated one.
- **Proper names are verbatim.** Technologies, formats, and algorithms keep their own spelling —
  `base64`, `utf8`, `json`, `sha256`, `url` — with only the casing following Promise convention.
- **Do not prefix a member with its type.** `response.status`, not `response.status_code`;
  `request.method`, not `request.request_method`. The caller already has the context.

## Types and shape

- **Private fields are `_`-prefixed; the public getter drops the underscore.** The underscore marks
  an implementation detail and signals that access goes through the getter.
- **Construction-only fields are `` `final ``.** It prevents later mutation, documents intent where
  it is declared, and lets the compiler assume the value never changes.
- **Construct through factory methods on the type** — `Response.ok(...)`, `Server.bind(...)` — not
  free functions. A factory can set `` `final `` fields and lives with the type's other methods.
- **A getter is a cost signal.** `get name T` means side-effect-free *and* cheap — field-like, O(1).
  Anything that allocates or computes is a method, even parameterless: `to_string()`, `clone()`,
  `bytes()`. The parentheses tell a caller that work happens. **Interface conformance overrides
  this**: where a `` `structural `` interface declares a getter, every implementor matches the form
  even if its own implementation is O(n).
- **A set that can be closed is closed.** An operating system is `linux` / `darwin` / `windows`, not
  a string — because a string admits `macOS`, and the failure is silent: a lookup that misses does
  not raise, it falls through to a default and runs the wrong thing. A closed set refuses the value
  at the boundary and names what was allowed instead.

  **Closed is also the only reversible choice.** Opening a set later accepts values that were
  previously refused, which breaks nothing. Closing one later refuses values already written down,
  which breaks everything holding them — and since the wire evolves additively, there is no version
  in which that becomes safe. So the question is never "might this need to grow?" but "must it be
  open *today*?"

  What stays open is what a project invents and the system only carries: tags, metric names, the leaf
  of an exclusion, the set of build targets. What closes is anything the system must *interpret* —
  if a value changes what the code does, its set is closed.
- **Absence is an optional, never a sentinel.** A field that is sometimes not there is `T?` — not an
  empty string, not a zero, not a `None` case bolted onto an enum, and not a `has_x` bool sitting
  beside the value it guards. Every one of those spellings admits a state that means nothing: complete
  *and* carrying a reason it was not, `has_fix` false *and* a fix command sitting in the field. An
  optional deletes those states rather than documenting them, and it puts the check where the compiler
  can insist on it instead of in a doc comment a caller may not read.

  The reach of that is wider than it looks: a target that means "the host it ran on" when empty, a
  schedule whose `None` case means "never scheduled", a preflight that is the empty command — all
  three were sentinels wearing a type. What is *not* absence is an empty collection: no arch filter,
  no tags, and no exclusions are real empty lists, and `T[]?` would only add a second way to say the
  same thing.

  Reading one back is not obvious from the errors, so: `if this._field is present { return
  this._field!.clone(); }`. `if v := this._field` moves out of the field, `!= none` is not defined,
  and an optional carries no members of its own. Inside an `if x is present` body a **local** is
  narrowed and needs no `!`; a field is not, and still does. Narrowing does not cross `&&` or reach
  the statement after the `if`, so nest the checks.
- **Identities are types, never bare `string` or `int`.** The
  [identity model](https://github.com/promise-language/reactor/blob/main/docs/design.md#identity) names eighteen distinct things, and the ones crossing
  an owner boundary are published here. A function taking three `string`s takes them in an order
  nothing checks, and an item id assigned to a project id is a bug the compiler should have caught.
  Identity types are value types, immutable, all fields `` `final ``.
- **A quantity is the standard library's type for it.** A timeout is a `Duration`, not `"30m"`; a
  moment is a `DateTime`, not an epoch `int`. A string holding a quantity has to be parsed at every
  use, cannot be compared or added, and pushes its format — `30m`, `30 min`, `PT30M` — onto everyone
  who writes one. The stdlib type parses once at the boundary and is a number everywhere after. This
  is the [reuse rule](#do-not-work-around-the-platform) applied to values rather than behaviour: if
  the quantity has no type yet, ask for one upstream rather than passing a string.

## Comments and documentation

- **`` `doc `` on every `` `public `` declaration.** It is the API surface that tooling and agents
  read; describe behaviour, not the signature.

  **A synthesized member is exempt, because it has no declaration to annotate.** A type marked
  `` `clone `` publishes a `clone` nobody wrote, and the annotation on the type is the documentation:
  it says the member exists, and the language says what it does. Writing the member out by hand to
  have somewhere to hang a `` `doc `` on gets that backwards — it trades a guarantee the compiler
  makes for a sentence a reader has to trust, and ten copies of *"An independent copy."* document
  nothing the annotation did not already say. **So prefer the annotation wherever it compiles**, and
  where it does not, the hand-written member is a workaround and its `` `doc `` says which defect it
  is waiting on.
- **No decorative banners.** `// ── Section ──` carries no meaning, costs tokens, and rots.
- **Default to no comments.** Names carry meaning. Comment the *why* when it is non-obvious — a
  hidden constraint, a subtle invariant, a workaround and the issue it waits on.
- **Documentation is part of the change, not after it.** A change that makes a document wrong is
  incomplete, and the [design corpus](https://github.com/promise-language/reactor/blob/main/tree/main/docs) is load-bearing rather than descriptive: its
  cross-references are claims meant to be checked.

> **No `TODO` comments. Fix it now, or file it and reference the issue.**

A `TODO` is a backlog entry filed somewhere with no backlog semantics: nothing lists them, nothing
ranks them, nothing closes them, and **nobody ever sweeps them**. It is the same
[mirrored-knowledge failure](https://github.com/promise-language/reactor/blob/main/docs/base-engineering.md#no-manual-gate-registration) the gate contract
exists to prevent — a second copy of "work that is not done", kept where the first copy cannot see
it.

The decay is the worst part. Stale `TODO`s train every reader to skip all of them, which destroys
the few that meant something. **The tracker is the single source of truth for undone work**; a
comment may *reference* an issue, and must never *be* the record.

> **No plans, phases, or task lists in documentation either. A document defines the destination; it
> never reports the distance travelled.**

The distinction is **normative versus record**, not present versus future. An architecture document
describes a system that does not exist yet — that is its whole job, and it is not status. *"A lease
names its holder as `(host, pid, start time)`"* is a claim about the design: equally true before any
code exists and after all of it does, and work happening never makes it stale. *"Phase 2: lease
ledger — done"* decays the moment anything moves.

**The test has an edge you can apply:** would this sentence need editing when work happens, even
though nothing about the design changed? If yes, it is a record, and it belongs where records live.

Mixing the two is what makes a document untrustworthy, and it fails the same way a `TODO` does. A
doc carrying *"Phase 1: … Phase 2: …"* is a backlog nothing closes, and it rots **silently** because
nothing breaks when it becomes wrong. The end state is a document describing as planned most of what
already shipped — worse than no document, since a reader who cannot tell which half is current has
to verify all of it and will trust none of it.

**So a document points at where status lives rather than reporting it.** What is not yet built is in
the tracker; what is built is in the code and its own documentation. *"See the open issues"* is
complete and permanently correct, where a checklist is correct for a week.

The corpus holds itself to this: progress lives in [the README's Status
section](https://github.com/promise-language/reactor/blob/main/README.md#status) alone, which is why nothing here carries a milestone section or a
per-section status banner. **A list of requests against another project is a record too** — it names
its upstream issues rather than describing their current state, so the tracker stays the authority
and a row retires when its issue closes.

## Do not work around the platform

> **When the language, compiler, runtime, or tooling is in the way, file it upstream and stop.**

Do not restructure code to dodge a parser bug, add redundant casts to sidestep a type-checker gap,
or reimplement something because a feature is missing. BASE is among the platform's first large
applications, and every workaround it absorbs is a defect the platform never learns about: a gap is
a platform request, not a local problem.

- **File against [promise-language/promise](https://github.com/promise-language/promise/issues)**
  with what broke, the workaround if one is unavoidable, and the priority.
- **An unavoidable workaround is commented and linked to its issue.** An undocumented workaround is
  indistinguishable from a mistake six months later.
- **Reuse the standard library.** If something is missing, ask for it upstream rather than growing a
  local copy. A local utility that duplicates a stdlib concept is the same defect as a workaround,
  with a longer half-life.

## Define once

> **Each piece of state lives in one place. Each behaviour is implemented once.**

- **Never two copies that must be kept in sync.** That is not a style preference: two copies are
  two things that can disagree, and nothing tells you when they have.
- **A genuine performance duplicate is an explicit cache**, with **one clear owner and defined
  invalidation** — never a second source of truth. The difference is that a cache knows it is
  derived and knows when it is wrong.
- **Implement a behaviour once and reuse it.** Do not copy an implementation and add minor
  variations; the variations are where the two copies start disagreeing. Always reach for the
  simplest single implementation.
- **Wire types are one shared module used by both
  sides** ([why](https://github.com/promise-language/reactor/blob/main/docs/design.md#seams-are-process-boundaries--by-design-not-by-accident)) — not
  hand-kept-in-sync copies, which is the entire reason that module exists.
- **The exception is deliberate vendoring**, as with this document — marked where it happens, with
  its source named.

## Keep a change to its subject

**Do not refactor or add unrelated features while implementing something else.** A change that
carries passengers is a change nobody can review: the reviewer cannot tell which edits the item
required and which were opinions, and a revert takes the passengers with it.

Improvements found along the way are worth having — as their own item, filed and linked, not folded
into the current one.

## Look for the silent classes

Correctness bugs announce themselves. These do not, so they are a standing obligation in **any code
a change touches** — not only in the lines it adds:

| | |
|---|---|
| **Memory leaks** | zero tolerance. Every heap-allocating type needs a drop path, and every leak is a regression rather than a pre-existing condition |
| **Lifetime errors** | double free, use after free, missing scope cleanup |
| **Concurrency races** | lock ordering, park/wake, channel close |
| **Resource waste** | handles, connections, and processes that are opened and never accounted for |

**Anything found here is filed at critical priority**, whether or not the current change caused it.
These are the classes that survive review, pass tests, and surface in production as something
unrelated — which for a system built to [run unattended for prolonged
periods](https://github.com/promise-language/reactor/blob/main/docs/design.md#objectives) is the failure mode that matters most.

## Visibility

- **Nothing is `` `public `` that does not have to be.** Module-only, or narrower, by default.
- Public is a commitment: it is what other modules compile against and what
  [wire compatibility](https://github.com/promise-language/reactor/blob/main/docs/design.md#a-shared-module-is-not-a-shared-version) constrains. Reaching for
  it early is how a private detail becomes a permanent one.

## Testing

- **Every behavioural change is tested** — the change itself, its **edge cases**, and its **error
  paths**. A test that covers only the happy path documents the feature and verifies almost nothing;
  the paths that go wrong are the ones nobody exercises by hand.
- **Audit resource invariants, not just line coverage.** Every heap-allocating type gets a test that
  confirms cleanup; concurrency code gets a stress test. A covered line that leaks is covered and
  wrong, and coverage percentage will never say so.
- **Genuinely untestable code is filed, not skipped.** Where something cannot be tested — it needs a
  network, an external process, or a language feature that does not exist — file it rather than
  leaving a silent hole, and say why in the item.
- **Prefer batch tests** — functions tagged `` `test `` using `assert()` — over snapshot tests. Cost
  is dominated by binaries compiled, not by execution, and batch tests compile into one.
- **Co-locate tests with the code they test.** `*_test.pr` beside the `.pr` file. A separate tree is
  for cross-cutting integration tests only.
- **Never use `sleep` for synchronization.** Order concurrent operations explicitly — a channel, a
  ready handshake, an awaited `Task` — never by sleeping and assuming an event happened. A sleep
  standing in for a happens-before edge is load-sensitive: the window that passes on an idle laptop
  collapses on a loaded runner, and no amount of lengthening fixes it. This is not a ban on testing
  timing *behaviour*; it is a ban on using a clock where a signal belongs. **What is built on this
  layer is unusually exposed here** — leases, long-polls, deadlines, and process supervision are
  most of what an orchestrator does.
- **Zero memory leaks, and the check never gets suppressed.** A leak is a regression rather than a
  pre-existing condition, and there is no annotation for tolerating one. This matters more here than
  in a batch program: what links these modules runs for weeks, so a leak it carries is unbounded
  rather than merely large.
- **Every wire contract has a conformance suite**, and every implementation of a store passes the
  same one — [the reason the persistence split is stated as an
  interface](https://github.com/promise-language/reactor/blob/main/docs/design.md#persistence) at all.

## No hidden effects

- Every effect is visible at the call site. If a function can fail it is marked failable; if a value
  is consumed it is moved; if a variable is mutable it says so.
- **Self-contained by default.** Avoid global state, implicit initialization, and ambient context. A
  reader — human or agent — should be able to read a file top to bottom and know what it does.

## Prompts point here; they do not restate this

A flow's step prompts are the natural place for these rules to be repeated, and repeating them there
is the same defect as everything else in this section: a prompt that restates a rule is a second
copy that drifts, and the one in the prompt wins because it is the one the agent read.

> **A step prompt cites this document. It does not carry its own copy of the rules.**

That works precisely because [the guide is in the tree](#why-this-is-here-rather-than-referenced) —
the agent resolving an item can read it. Prompts stay about the *task*: what this step is for, what
its artifact is, what it must not do.

**And most of what a prompt would otherwise repeat should be a grant instead.** Instructions like
*do not commit*, *do not push* are prompt-shaped requests for what
[role ∩ step](https://github.com/promise-language/reactor/blob/main/docs/design.md#authority-roles-steps-and-capabilities) makes mechanical — the design's own
point is that this turns "this step should only do X" from an instruction an agent may ignore into a
boundary it cannot cross. A rule that could be a bound and is written as a sentence is a rule that
will eventually be ignored exactly once.

## Enforcement

The corpus's own rule is that [a grant with no choke point behind it is advisory and should be
labelled as such](https://github.com/promise-language/reactor/blob/main/docs/design.md#where-it-is-enforced). The same honesty applies here: most of this is
not yet checked by anything.

| Rule | Enforcement | Status |
|---|---|---|
| Approved abbreviations | lint against the §9.3a dictionary | **not built** |
| `` `final `` on construction-only fields | lint | **not built** |
| `` `doc `` on every `` `public `` | lint | **not built** |
| Nothing needlessly `` `public `` | lint — public with no external use | **not built** |
| Workaround without a linked issue | lint | **not built** |
| No `TODO` comments | lint — trivially checkable | **not built** |
| No plans or checklists in docs | lint — checkbox lists, `Phase N` headings | **not built** |
| Test coverage | gate, with a ratcheted baseline | **not built** |
| Zero leaks | gate — already the platform's posture | **not built here** |
| Documentation links resolve | precommit gate | [filed against reactor](https://github.com/promise-language/reactor/issues/1); wanted here too |
| Define once · no hidden effects · identity typing | review | advisory |
| Documentation currency beyond links | review | advisory |

**None of it can be built yet.** A gate is a program that takes arguments, and `promise run` passes
none — the [platform request](https://github.com/promise-language/reactor/blob/main/docs/design.md#platform-requirements--requested-of-promise)
that blocks every Promise dev tool, and therefore `bin/gate list --json`, the one command BASE asks
of a project. Writing these as Go tools instead would rebuild the machinery the
[Promise tooling model](https://github.com/promise-language/reactor/blob/main/docs/dev-tooling.md)
exists to delete.

So the rules stand as review obligations until that lands, and **the table is the roadmap**: each
row becomes a gate when it can, rather than a rule someone remembers.

**When a gate is built, it is built here.** These rules govern every BASE repository, so a lint that
enforces one belongs beside the guide that states it rather than being written once per project —
which is the same argument this document makes about itself.
