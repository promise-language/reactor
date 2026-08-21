# The engagement feed

> **This document defines how the system spends human attention** — the article, ranking by regret
> per minute, questions and their deadlines, and what a reader is permitted to do from a card.
>
> **It assumes** [design.md](design.md)'s authority model and persistence split, and
> [base-engineering.md](base-engineering.md)'s step resolution.
> **Depending on it:** nothing yet; design.md carries a summary and links here for the schema.
>
> What is undecided is in [Open questions](#open-questions); everything else here is a statement
> about the system.

## Goal

Define one **abstract, well-defined engagement surface** that any component in the system — a flow
step, a gate, an item question, an item concern, Reactor itself, or a future component nobody has
written yet — can post to without Reactor needing to know that component exists.

The core unit is a **feed article**: a durably-identified, self-describing call to attention. The
user reads a single ranked **feed** (the "Feed" tab of the Reactor UI), where articles are ordered
by **how much it costs to leave them undone, per minute of attention they take** — see
[Ranking](#ranking--regret-per-minute-of-attention). The user can **dismiss** an article, **take
one of its calls to action**, or **navigate** to whatever it references.

This is the mechanism behind the white paper's [inbox → feed](../WHITEPAPER.md) direction:
*"a social-media-style stream the human engages with on their own schedule, while the resolution
loop keeps running underneath."* Human attention is the scarcest resource, and the system is
engineered to spend as little of it as possible.

**A river, not a ledger.** The feed is ephemeral by design, like a social timeline: an article is
either engaged with *now* or it flows away. There is no archive and no permanent read-history — you
cannot scroll back to what you read two years ago. Articles leave when nothing is lost by ignoring
them, or by being dismissed, resolved, or expired. **Nothing is retained forever.** This shapes
every storage and UI decision below: the system optimizes for "what deserves attention now," not
for recall.

The design rests on two properties. First, the article *kind* is open-ended: the component
identifier is a registry-backed value, not a closed enum, so an unforeseen component can post after
a data-level registration — no Reactor code change. Second, impact, audience, and calls-to-action
are first-class, creator-controlled fields rather than Reactor-side conditionals — while the inputs
a creator *cannot* know, above all what work is blocked behind it, are computed by Reactor.
Identifiers that would otherwise drift into free-text soup (component, audience, tags) are kept to
controlled vocabularies — stamped at the boundary and canonicalized on ingest (see
[Identifiers](#identifiers-controlled-vocabularies-not-free-text)).

### What this proposal adds

Mirroring the [gate contract's](base-engineering.md#gate-output-envelope) split of concerns:

1. A **stable wire format** for a feed article (`flow:feed-article-v1`) — a single JSON envelope any
   language can emit, living in the
   [BASE layer wire module](design.md#seams-are-process-boundaries--by-design-not-by-accident)
   alongside the flow↔Reactor types and the gate envelope.
2. **Emission channels** that route an article from a component to Reactor: a post call on the flow
   API, an `articles` array on the gate output envelope, an MCP `post_article` tool, and the REST
   sink `POST /api/feed`.
3. A **ranking contract** — regret per minute of attention — defined here so every deployment
   ranks identically.
4. **Reactor-side machinery** — the store, the ranker, the sweep, the feed API, and the Feed tab.

The contract is **the JSON, not any SDK**. A Promise convenience package may exist for typed
emitters, but an article that depends on that package's *existence* has broken the contract — the
same rule the [gate boundary](design.md#language) already lives by, and the resolution of the
original proposal's open question 6.

## Feed-held state is an optimization, never authority

> **Feed-held state is an optimization, never authority.** Wipe the entire feed store and nothing is
> lost but attention.

This is deliberately the same sentence the design applies to arenas —
[*"arena-held state is an optimization, never authority: every fact a correctness decision rests on
must already have been streamed to the server or
committed"*](design.md#a-host-that-is-merely-off-is-not-a-host-that-is-gone).
The feed is a second store with the same property, so there is no new principle to learn.

The test it must pass: with the feed store emptied, questions are still parked on their items,
answers still recorded with their authors, gates still red, items still blocked. The feed rebuilds
and the fleet does not notice.

That preserves the river completely. The feed still keeps nothing, still decays, still has no
archive — it simply stops being the only copy.

**Post-worthy implies record-worthy.** Nothing lands in the feed first. An article always projects
something already durable elsewhere: an item annotation, a gate result, a ledger record. A component
wanting to post something recorded nowhere is a signal that the thing should be recorded first —
not that the feed needs to remember it. Every case in the mapping table below already satisfies
this.

### Two article classes

They differ by who maintains them, and they fall along the dichotomy the design already uses for
[every piece of persisted state](design.md#every-exclusion-is-held-by-a-process-never-by-a-flag)
— **held** or **timed**, with no third form.

| Class | Identity | Lifetime | Examples |
|---|---|---|---|
| **Condition** | derived key | **held** — exists while the condition is asserted | gate X is red · item #481 is parked on a question · arena Y is absent |
| **Event** | per-occurrence key | **timed** — leaves once ignoring it costs nothing | today's work summary · a run finished |

**A condition article's key is a pure function of the condition it projects.** That is the
[branch-naming rule](base-engineering.md#branches-are-mechanical-and-there-is-exactly-one-per-claim)
— *a pure function of the claim; nothing smart chooses it* — applied to article identity. Two posts
about one red gate collide because they cannot do anything else; there is no room for a run number,
timestamp, or attempt counter to leak into the key and produce a second banner for one condition.

**Retraction stops being an act.** A condition article exists while something asserts the condition
and disappears when the assertion stops. It inherits the property that matters from the lease rule:
*there is no release code path to get wrong, because the case that matters is the one where the
holder never got to run its cleanup.* A gate going green retracts explicitly (the fast path); a gate
whose process dies mid-run leaves an assertion that lapses (the safety net). Neither leaves a
permanent red banner nobody can clear.

**Reconciliation, not appending.** A periodic pass ensures every standing condition has exactly one
live article and retracts articles whose condition has cleared. This needs no new loop: the slow
tick that already answers
[*is this unblocked?*](design.md#an-edge-names-a-target-and-a-condition-never-a-version) is the
natural home. Controller-shaped — desired state versus actual — which is what makes "wipe the
feed" genuinely safe rather than merely survivable.

### Questions, answers, and history

A parked question is a condition, so it inherits all of the above: key derived from
`(item, question id)`, one article by construction, retracted when the answer lands.

The durable records live on the **item**, never in the feed:

| Record | Holds | Why it must be durable |
|---|---|---|
| **question** annotation | id, text, options, recommendation, mode, deadline, `answerable_by`, `addressed_to`, owning step | the resumed step reconstructs what was asked; [context is assembled from durable artifacts at dispatch](base-engineering.md#context-is-assembled-never-accumulated) |
| **answer** annotation | question id, selection and/or free text, **author**, **arrival path**, timestamp | it steers autonomous work — a decision with an author |
| **checkpoint** | the step's partial work | so an unblocked step resumes rather than restarts |

Storage is composite, exactly like items: the human-readable question and answer are issue comments
(so a contributor reads and answers where they already work); the structured form is the
[private overlay](design.md#itemstore--composite-identity-github--private-overlay) keyed by the
same id.

**Arrival path is not bookkeeping.** "A human picked the recommendation" and "the deadline fired and
took the recommendation" produce an identical selection and mean entirely different things. A record
that cannot distinguish them shows a human decision that never happened.

**Free text is the default; only Reactor may take it away.** A step that could say *answer only A or
B* would be the constrained party narrowing its supervisor's reply — the same self-authorizing shape
the design rejects when
[a flow declares its own grants](design.md#what-a-flow-declares-and-what-is-declared-about-it).
A step proposes options; it may not restrict the reply. `closed_form` is legitimate in exactly one
case — when **Reactor itself** consumes the answer rather than handing it to an agent (a budget
approval, a "declare this arena lost"), where prose is ambiguous and the server acts on it directly.
Everything else is read by a step, and a step is an agent that can interpret *"none of these, do D,
because the second one breaks the WASM target"* — the answer you most want to be able to give and
the one a closed enum destroys.

**An answer is input, not authorization.** It feeds the work; it never feeds the bound. Effective
authority stays [role ∩ step](design.md#authority-roles-steps-and-capabilities) regardless of
what any answer says, and a human wanting to widen a grant does it in companion-repo config, not in
a comment an agent parses.

**Dismiss ≠ answer.** Dismissing a question's article does not answer it — the park stands and the
article returns on the next reconcile, or a human clearing their feed would silently abandon blocked
work. But a dismiss on an *addressed* article is a decline: it lapses the `addressed_to` preference
immediately and the article returns addressed to the role. The obligation moves; it never
evaporates.

**History is not the feed's.** The feed keeps none, so it can never disagree with history. "Gate X
was red Tuesday to Thursday, then again Friday" is answered from gate run history and the repair
item. An article carries a **reference to the durable occurrence** rather than an occurrence-scoped
key, so the same key spans episodes and green-then-red-again is a new instance under that key with a
fresh `created_at`.

## The article

### Schema (v1, JSON)

```jsonc
// flow:feed-article-v1
{
  "schema_version": 1,

  // Identity — chosen by the creator, stable across re-posts.
  "key":    "gate:build-time",        // durable id, creator-namespaced; derived for condition articles
  "source": { },                      // who created this (component + optional item/agent/gate/step ref)

  // Content.
  "title":       "Build time regressed 18%",
  "description": "markdown body",
  "media":       [ ],                 // ordered; [0] is primary (loud), rest subdued

  // Calls to action — one primary + ordered alternatives, or a choice set.
  "actions": [ ],

  // Ranking inputs the creator can honestly know. Everything else is computed.
  "impact_hours":   8,                // work-hours at risk if this goes wrong. 1 / 8 / 40 / 200
  "attention_cost": "1m",             // flow:duration; omitted = derived from the primary action kind

  // Audience and grouping — structured, not free text.
  "audience": { },                    // omitted = everyone
  "tags":     [ "topic:perf" ],       // "namespace:value", canonicalized; advisory only

  // Server-stamped on ingest; carried for the read model only.
  "created_at": "RFC3339",
  "expires_at": "RFC3339"
}
```

| Field | Meaning |
|---|---|
| `schema_version` | wire version. Unknown majors are **refused**; evolution within a major is **additive-only** — [the standing rule for every wire contract](design.md#a-shared-module-is-not-a-shared-version). |
| `key` | durable identity, creator-namespaced. For a **condition** article it is a pure function of the condition. |
| `source` | who created it — stamped at the boundary, never claimed (see [Source](#source--who-created-it)). |
| `title` / `description` | card headline and markdown body. |
| `media` | ordered attachments; the first is primary. |
| `actions` | the calls to action (see [Action](#action--the-calls-to-action)). |
| `impact_hours` | **work-hours at risk** if this goes wrong. Scales the ranking terms; it is not the score. |
| `attention_cost` | expected human minutes to dispose of it. Omitted ≡ derived from the primary action kind. |
| `audience` | routing — whose feed it ranks into. **Not a permission.** |
| `tags` | advisory display/filter facets. |
| `created_at` / `expires_at` | **server-stamped.** Emitters have no trustworthy clock and no monotonicity guarantee across hosts. |

### Identifiers: controlled vocabularies, not free text

`source`, `audience`, and `tags` carry the identifiers Reactor attributes, groups, and filters by.
Left as raw free strings they rot into synonym soup (`perf` / `performance` / `perf-regression`) and
every filter silently misses. Three rules keep them clean *without* a closed enum, which would
defeat the open-extension goal:

1. **Stamp at the boundary, don't type by hand.** The emission channel fills identifiers from what
   the runtime already knows: the flow API stamps `source{component:"flow", name:<flow>,
   step:<step>, item_id:<item>}`; gate ingest stamps `source{component:"gate", name:<gate>}`. A flow
   or gate author never spells its own `source`. Only the MCP and REST sinks accept an
   author-supplied `source` — and only there does rule 2 bite.
2. **Canonicalize on ingest.** Author-supplied identifiers are lowercased, kebab-cased, and trimmed
   before storage. Tags additionally take a `namespace:value` shape (both `[a-z0-9-]+`) so related
   labels cluster under a known facet.
3. **Registry, not enum; advisory, not behavioral.** Two registries — `component` values and tag
   namespaces — live in a **Reactor-owned registry**, seeded with the built-ins and extended by
   config/registration, not a code change. Reactor **never branches behavior** on either; they drive
   attribution, grouping, and filtering only.

   **Audience roles are not one of them.** The feed
   [consumes the system's role vocabulary](#audience-and-tags) rather than registering its own, so
   there is nothing to seed and nothing that can drift out of step with the authority model.

   Seeds are deliberately small. `component` is **`flow` · `gate` · `reactor`** — exactly the three
   things that talk to the Reactor API, so the set is derived from the seam model rather than
   invented. Tag namespaces are **`topic:` · `area:` · `severity:`**; anything naming the project,
   item, or gate duplicates what `source` and `key` already carry.

   **Component says who posted; `key` says what it is about.** Filtering by subject — *show me
   every open question* — comes from display facets Reactor derives from key structure
   (`item:*:question`), which is free and needs no registry. Those facets are **display only,
   never behavior**, exactly like tags: a key that does not follow the convention loses a chip and
   nothing else.

### A degraded path is never a silent path

An unregistered identifier is always a defect — something named a role, component, or namespace that
does not exist. Degrading gracefully and reporting the defect are **two obligations, not a choice
between them**, the same way [an orphaned grandchild is a reported fault rather than silent
debris](design.md#nothing-runs-unwatched). What varies is severity; what never varies is silence.

| Identifier | Nature | On unknown value |
|---|---|---|
| `answerable_by` | authority | **fails closed** — nobody may answer, and the question escalates |
| `audience.role` | routing | **fails open** — delivered to everyone in read scope, and a fault is raised |
| `source.component` | attribution | delivered, rendered flagged, and a fault is raised |
| tag `namespace` | cosmetic | delivered, chip flagged, and a low-impact event that leaves on its own |

The fault is itself an article, and the machinery from
[condition keys](#two-article-classes) keeps it from becoming its own noise problem: keyed on the
unknown value (`config:unknown-role:<name>`), so five hundred mis-routed articles raise **one**
fault, held while any article still references the unknown name and retracted when the name is
registered or the last referent ages out.

### Source — who created it

```jsonc
{
  "component": "gate",          // registered id: flow | gate | reactor | <registered>
  "name":      "build-time",    // flow/gate/breaker name — stamped by the channel, canonicalized
  "item_id":   "T0481",
  "agent":     "verifier-1",
  "step":      "implement"      // when component == "flow"
}
```

`component` is a **registered identifier** — neither an arbitrary string nor a closed enum. On the
flow and gate paths the channel stamps it, so authors never spell it; on the MCP/REST paths it
resolves against the component registry, where an unforeseen component registers once and is
thereafter known. Reactor uses `source` only for attribution display and to resolve
navigate-to-source actions.

**`source` is stamped, never claimed** — and that is load-bearing rather than tidy, because the feed
is a channel from low-trust agents to high-trust humans (see
[Authority](#authority-over-article-actions)). An article that could lie about its origin would make
provenance worthless exactly where it is relied on.

### Media — an ordered list of attachments

`media` is a plain ordered list. **The first attachment is the primary** — the one the card presents
loudly (inline thumbnail/link); the rest are subdued behind a disclosure. No `primary`/`secondary`
fields and no fixed cap: ordering carries the emphasis.

```jsonc
{ "kind": "link", "label": "Trend chart", "url": "https://…" }
// kind: "link" | "doc" | "image" | "file" | "item" | "patch"
// reference: internal reference (item id, artifact id, patch hash, …)
//            when kind in {doc, item, patch}
```

| `kind` | What the reader gets |
|---|---|
| `link` | a destination — click to leave |
| **`doc`** | **a markdown document, rendered in place** |
| `image` | an image, inline |
| `file` | something to download |
| `item` | a linked item, rendered as a card |
| `patch` | a diff, rendered as one |

**`doc` earns its own kind because markdown is the canonical user-facing format.** The artifacts a
step actually produces — a plan, an inspection, a review, an answer — are markdown, so attaching one
should *render* it, not offer a download of it. That is the whole difference from `file`, and from
`link` it is the difference between content and a destination: a `doc` is part of what the reader is
being asked to consider, not somewhere else to go.

Bytes are **not** embedded in the article — the same rule as the gate output envelope. Every kind
carries a reference: a `doc` names an item artifact or a Reactor-served blob, an `image`/`file`
points at an external URL or a blob uploaded separately. The one inline exception is the article's
own `description`, which is markdown by the same reasoning.

### Action — the call(s) to action

This generalizes hardcoded "Create Bug / Create Task / Open item" buttons, plus the multiple-choice
shape of an item question.

```jsonc
{
  "id":       "file-bug",        // stable within the article
  "label":    "File bug",        // button text
  "kind":     "operation",       // navigate | choice | operation | external
  "primary":  true,              // at most one; rendered prominently

  "destructive": false,          // irreversible/harmful: caution styling
  "confirm":     false,          // gate behind a confirm dialog before firing
  "explain":     "Files a `perf`-tagged bug linked to this gate and assigns it to you.",
  "after":       "resolve",      // keep | dismiss | resolve — article fate once taken

  // Per-kind payload (only the field matching `kind` is read):
  "navigate":  { "target": "gate", "reference": "build-time" },
  "choice":    { "options": ["A","B"], "multi_select": false, "closed_form": false },
  "operation": { "name": "create-bug", "parameters": { "tag": "perf" } },
  "url":       "https://…"
}
```

| `kind` | Effect |
|---|---|
| `navigate` | open something in the UI — an item, the source gate/agent, a URL panel |
| `choice` | present a single- or multi-choice; the selection is recorded durably (see below) |
| `operation` | invoke a named, allow-listed Reactor operation (create-task, create-bug, run-gate, release-lease, …) |
| `external` | open an external URL |

**Dismiss is always implicitly available** — it never needs to be listed.

**`explain`, `destructive`, `confirm` — make the consequence legible before the click. The two flags
are independent:**

- **`explain`** is a short markdown sentence describing the *effect*. `label` stays terse for the
  button; `explain` carries the detail.
- **`destructive`** is pure presentation: an irreversible or harmful action rendered in caution
  styling so it reads as dangerous at a glance. It says nothing about confirmation.
- **`confirm`** gates the action behind a confirmation dialog. This is for anything the user should
  not trigger by a stray click — including actions that are **expensive or slow but perfectly safe**
  (run the full CI suite, kick off a paid build).

They compose: `destructive` alone is a scary-looking one-click button; `confirm` alone is a calm
button that asks "are you sure?"; both is the dangerous *and* gated case. As a safety floor Reactor
**also confirms `destructive` actions even if `confirm` is unset** — a destructive action is never
one-click — so in practice `confirm` means "gate this *even though* it isn't destructive."

When a confirmation is required, the dialog shows `explain` as its body, **names the article's
`source`** so the human knows who is asking, and its affirmative button **echoes the action's
`label`** (*"Run full CI"*, *"File bug"*) rather than a generic OK, paired with *Cancel*.

**What happens to the article after an action — `after`.** Taking an action does not remove the
article by default: you might "Open gate" just to look while the condition is still live.

- **`keep`** (default) — navigate/external "go look" actions.
- **`dismiss`** — the action *is* the acknowledgement ("Got it"); the article is removed.
- **`resolve`** — the action handles the underlying condition, so the article retracts exactly like
  a creator resolve. Choice actions default to `resolve` once a selection is recorded; everything
  else defaults to `keep`.

The always-available implicit **Dismiss** is independent of any action's `after`.

### Impact and attention cost

These are the two ranking inputs a creator can honestly know. Everything else the ranker needs is
either measured (time) or computed from state the creator cannot see (what is blocked behind it).

**`impact_hours` is the work at risk if this goes wrong** — not the score, and not a request for
position in the list.

> **Denominating it in real units is what makes the whole model interpretable.** A bare 0–100 weight
> produces a score whose absolute value means nothing, so the fold is an ordering artifact and
> "impact was 50" is a claim nobody can be wrong about. In work-hours, `rank` becomes a literal rate
> — **work-hours saved per minute of human attention** — which is the white paper's thesis made
> computable, and "we lost forty hours to that default" is a claim calibration can check.

| Anchor | Meaning |
|---|---|
| 1 | trivial — an hour's rework |
| 8 | a day |
| 40 | a week |
| 200 | a month |

Any positive number is valid and the anchors are only reference points, so an emitter can say `3`
or `60` when that is the honest estimate. **Numeric only** — no name sugar on the wire: names would
resolve against deployment config, so the same emitted article would mean different things in
different fleets, and a union type is permanent under additive-only evolution.

**Money folds in by a deployment rate, and both stay visible.** Spend genuinely at risk — API budget
already committed to work that a wrong answer discards — converts to hours at a configured rate for
*ordering only*. Every card shows the components unscalarized (*"blocking 11 items · ~40 work-hours
at risk · $120 agent spend"*), because [two currencies, not
one](design.md#two-currencies-not-one) refuses to hide the tradeoff being managed — and ranking
needing a single number is not a reason to stop showing both.

**`attention_cost` is the expected human minutes to dispose of it** — and it is a first-class
ranking input, not metadata. A one-click choice is a minute; *"review this design proposal"* is
twenty. Omitted, it is derived from the primary action kind, which is right often enough that most
emitters should leave it out:

| Primary action | Default |
|---|---|
| `choice` with options | 1m |
| `navigate` / `external` | 2m |
| `operation` | 2m |
| no actions (informational) | 30s |

An emitter that knows better may override, and observed time-to-action
[calibrates the defaults](#the-feedback-loop-calibrates-estimates-never-the-objective) over time.
Both are `flow:duration` strings (`"90s"`, `"20m"`) to keep the wire language-neutral.

### Audience and tags

```jsonc
{ "role": "reviewer", "reference": "alex" }   // omitted/empty = everyone
```

The mess in "who is this for" comes from per-person free strings. Targeting by **role** keeps the
vocabulary tiny and stable; an optional `reference` names a specific principal *within* that role.
"For me" is the filter `role ∈ <the viewer's roles>` — there is no place to type a raw username.

**The feed consumes the system's role vocabulary; it defines none of its own.**

> **The role vocabulary is deployment-owned. The grants attached to each role stay project-owned.**

One name set, many grant sets. A companion repo declares *"role `reviewer` may run these steps with
these capabilities **here**"*; it does not mint the name. This amends
[design.md](design.md#what-a-flow-declares-and-what-is-declared-about-it), which currently places
roles themselves in companion-repo config, and it resolves a tension the design already carried —
[admin access control](design.md#configstore--the-deployment-owners-residual) was deployment-wide
while roles were per project, so "who a principal is" and "what roles exist" answered to different
owners.

Three reasons that way round:

- **A principal is a deployment-level thing.** They hold an account on the Reactor server. If two
  projects each define `reviewer` differently and one human holds both, "who is this person" has two
  answers and *for me* has none.
- **The bound does not weaken.** Vocabulary and grants both still sit outside the project worktree,
  so [an agent still cannot widen its own role](design.md#the-capability-vocabulary).
- **Every role stays visible to the deployment owner**, which is the same reason
  [roles are flat and explicit rather than inherited](design.md#the-capability-vocabulary) — a
  role nobody central can see is a role nobody reviews. The cost is that a role only one project
  uses is still declared centrally, and that is the right trade for the same reason.

Two consequences the feed forces into the open:

- **A principal holds a role per project**, plus one at deployment scope for fleet-level conditions
  (an arena absent, a governor crash-looping, quota exhausted). `role ∩ step` therefore means *the
  role in the item's project* — a natural reading of the design that was nowhere stated.
- **An article resolves its audience in the scope of its source** — project scope for item- and
  gate-sourced articles, deployment scope for fleet-sourced ones. `source` already carries what is
  needed to tell which.

**Audience is routing, not authority.** It decides whose feed an article ranks into. It does not
decide who may see it (read scope does) and it does not decide who may act on it (see
[Authority](#authority-over-article-actions)).

**There is no `agent` audience.** The feed is a human surface. Under
[context assembled at dispatch](base-engineering.md#context-is-assembled-never-accumulated) a
step never reads a feed, it reads durable item state — so an article addressed to an agent is a
category error, and worse, an unvalidated channel into an agent's context that bypasses the
assembled-context rule. A component with something to tell a step records it;
[post-worthy implies record-worthy](#feed-held-state-is-an-optimization-never-authority) already
says where.

**Tags** are advisory display/filter facets, each shaped `namespace:value` (`topic:perf`,
`area:build`, `severity:regression`). Two hard rules keep the tag set from rotting:

- **Tags never drive behavior** — only chips and filters. A typo costs a filter hit, nothing more.
- **Don't duplicate what `source`/`key` already say.** Component, item, gate, and agent are facets
  Reactor generates for free; tags carry only cross-cutting themes.

## Ranking — regret per minute of attention

A social feed ranks by relevance because its objective is engagement. This feed's objective is the
opposite — [spend as little human attention as possible](../WHITEPAPER.md) — which makes it a
**scheduling problem under a scarce resource** rather than a relevance problem. What it minimizes is

> **the total cost of decisions not made.**

Say that plainly and the ranking function is forced, and it is not importance:

```
rank(a, t) = regret(a, t) / attention_cost(a)        // work-hours saved per minute of attention
```

**The denominator is not decoration.** A `Critical` item needing an hour may deserve to rank *below*
three cheap decisions that each unblock a project, because in that hour you could clear all three.
Ranking by importance alone systematically over-ranks expensive items and starves cheap high-value
ones — which is precisely how a human spends their scarce hour badly while three teams stay blocked.

### Regret has exactly three sources

```
regret(a,t) = work_at_risk(a,t)                       // accrual
            + P(window closes) × P(default wrong) × cost_if_wrong   // irreversibility
            + residual_value(a,t)                      // obsolescence
```

| Term | Shape over time | The case it models |
|---|---|---|
| **Accrual** | continuous, **growing** | work piling up behind a blocker |
| **Irreversibility** | ~zero, then a **cliff** | a window closing — a default fires, or work is wasted |
| **Obsolescence** | monotonically → **zero** | information whose usefulness is expiring |

`impact_hours` is not a fourth term — it **scales** the first two. *How bad is this* and *how fast
is it getting bad* are different questions, and a single priority number answers neither. Every term
is in work-hours, so `regret` is too, and the rate the ranker produces is one a human can argue
with.

**Decay is not a mechanism here; it is what zero regret looks like.** News sinks not because it is
old but because the cost of not reading it approaches zero. A red gate stays up because its cost of
inaction is constant or growing. A deadline question is quiet, then loud, because its
irreversibility term is a cliff. **Three behaviours, one quantity, no flags** — no decay constant
to tune, nothing to pin above the fold, no floor to configure. A design that needs those is one
where regret is not being modelled, and each of them is a patch over the term that is missing.

### Downstream weight is computed, never declared

The single most important input is the one an emitter cannot know. A `plan` step asking a question
has no idea whether three items in two other projects are parked behind it.

**Reactor does.** It owns the blocking graph —
[change sets and blocking edges](design.md#cross-project-work--change-sets-and-blocking-edges),
parks, the integration lock, arena bindings. So `work_at_risk` is a graph query over a quantity
[design.md defines](design.md#every-attempt-must-make-progress): the work already **sunk** into every
blocked item, measured from the ledger, plus the work still estimated to **remain**, weighted by the
evidence behind the estimate. Summed for ranking, shown separately on the card — a sum of a
measurement and an estimate is not itself a measurement.

A question blocking eleven transitive items with real spend behind them therefore ranks itself, and
**rises on its own** with nobody re-posting: more items pile up behind it, and the work already sunk
grows more at risk as trunk moves under it. That is the accrual term doing what
[trunk-red preemption](design.md#gate-execution--reactors-half) already does for landings,
generalized.

The same rule bounds the obvious gaming: declared `impact_hours` will inflate, so **compute what can
be computed** and accept declarations only for what genuinely cannot be. A real unit helps here too
— inflating "impact 50 → 100" is costless, while claiming a question puts a month of work at risk is
a statement that reads as false to anyone looking.

### The fold is a budget line, not a threshold

Hiding below a fixed score floor is wrong in both directions — a quiet day shows an empty feed to a
human who has time; a busy day shows a wall. Whether to show something depends on **whether there is
a better use of the next minute**, never on an absolute number.

> **Fill the feed until cumulative `attention_cost` reaches the reader's available attention budget.
> The fold falls where the budget runs out.**

Feed length then adapts to bandwidth: the same human with ten minutes and with two hours gets two
different, both correct, lists. Everything past the line is still reachable — it is a fold, not a
deletion.

**The budget is a per-role default in
[ConfigStore](design.md#configstore--the-deployment-owners-residual), overridable by the reader for
a session.** Declared data rather than inference, and since the budget moves only the fold and never
the ranking, a wrong value costs one *"show more"* click — which is the right price for not
guessing at someone's day. Observed session length can calibrate the default later, under the same
bounds as every other estimate here.

**The top-ranked article always shows, even if it alone exceeds the budget.** Otherwise the most
consequential item becomes permanently invisible to the busiest reader, which inverts the whole
point.

### Seeing something once is a delivery guarantee, not a ranking input

Novelty is a real need and a terrible ranking principle, so it is not one here:

> **Every article is surfaced once regardless of score. After that, regret governs.**

Conflating the two is how a trivial new notice outranks a question that is about to decide itself.

### The feedback loop calibrates estimates, never the objective

| Signal | What it calibrates |
|---|---|
| acted | the estimate was good |
| **time from open to action** | `attention_cost` — the quantity the system is worst at, and the only way to learn it |
| dismissed without acting | regret over-estimated, or mis-routed |
| read, not acted, returned later | `attention_cost` underestimated |
| expired or defaulted unseen | under-ranked, or the budget was genuinely exhausted |
| a source's articles consistently dismissed | that **source's** declared `impact_hours`, discounted — per source, never per article, and reported rather than applied silently |

> **Feedback tunes the inputs. It must never touch what is being optimized.**

That is the guardrail, and it is the difference between this and every feed it superficially
resembles. A social feed's loop optimizes engagement, which is why social feeds are compelling and
useless. **Here, more engagement for the same outcome is a worse feed.** The success metric is
*attention spent per decision correctly made*, minimized — and the ground-truth loss is **decisions
that expired or defaulted which the human, shown them later, would have decided differently.**
Everything in the table above is a proxy for it.

#### Collecting the ground truth without spending what it saves

Two sources, because the free one is not enough on its own:

- **Reversals cost nothing.** A default that fires and is later undone — the question reopened, the
  work reverted, a bug filed against it — is ground truth with no attention spent and no new
  surface. Honest, but sparse and lagging, and blind to a default that was wrong and nobody caught.
- **Sampled review, under a bounded allowance.** A small configured share of the attention budget
  (start at 5%) funds asking a human, retrospectively, whether a sample of fired defaults would have
  gone the other way — **spent only from budget the ranked feed did not consume**, so calibration
  can never displace real work.

**The allowance is necessary rather than tidy, because a calibration prompt has zero regret by
construction.** Nothing is blocked on it and no window closes, so under `rank = regret /
attention_cost` it would never surface and the model could never learn. Making it just another
article is the one place this design cannot self-apply — and the honest fix is an explicit, capped,
visibly-labelled allowance rather than a fictional regret term inflated until it ranks.

That it is calibration must be stated on the card. It spends human attention on improving the
system rather than on the work, which a deployment is entitled to decline outright.

### What v1 actually needs

Less than the model suggests — two small constants tables and one graph query:

| Input | v1 |
|---|---|
| `work_at_risk` | one graph query over the ledger, plus per-kind completion history for the estimated half |
| window / `remaining` | already on the article |
| `attention_cost` | the defaults table above, calibrated from observed time-to-action |
| `P(default wrong)` | a constant per question kind, calibrated from reversals and sampled review |
| `impact_hours` | declared, discounted by source calibration |

**The honest risk:** a bad `attention_cost` estimate is worse than none, because it buries or floods
a whole class at once. So estimates are bounded — no calibration may move an article more than an
order of magnitude from its declared inputs — and the bound widens only as data accumulates. Same
posture as [ratcheted baselines](base-engineering.md): let a mechanism tighten as evidence
arrives rather than trusting it on day one.

The anchors, the attention-cost defaults, and the calibration bounds are deployment config in
[ConfigStore](design.md#configstore--the-deployment-owners-residual), not contract constants —
the same division of labour as gate ratchet caps.

## Durable identity, supersede, and resolve

`key` is creator-chosen and namespaced (`"gate:build-time"`, `"item:T0481:concern"`,
`"breaker:work-stalled"`), and **derived rather than chosen for condition articles**. It replaces an
inferred dedup tuple with an explicit, single field.

- **First post of a `key`** creates the article; Reactor stamps `created_at`.
- **Re-post of an existing `key`** *supersedes in place*: content, actions, and impact are replaced.
  A `freshen` flag chooses whether the article counts as a new episode — resetting `created_at` and
  re-arming the [one-look guarantee](#seeing-something-once-is-a-delivery-guarantee-not-a-ranking-input)
  ("this got worse again") — or as an update to the same one ("same condition, new details").
- **Resolve** is the creator's explicit retraction and the **one** creator-side removal verb. It
  covers both "the underlying condition is gone" and "this article is no longer relevant." Resolve ≠
  dismiss: resolve is the *creator* retracting, dismiss is the *user* acknowledging.

**`key` + supersede is also the attention-spam bound.** A component's footprint is its distinct
keys, not its post rate — which is why condition keys being derived matters twice over. Per-source
key caps cover a component that mints unbounded keys.

## Authority over article actions

The original proposal was written before authority was considered. This section is that half.

> **An article is a shortcut, never a grant.** Taking an action performs the underlying operation
> *as the acting principal*, checked exactly as if they had called that endpoint directly.

That check is [role ∩ action](design.md#a-human-acting-directly-is-bounded-the-same-way), the
human-work form of the same intersection that bounds a step. **An article names an operation, never
a capability** — it is posted by an agent, and an article carrying its own grant would be the
constrained party writing its own permission slip.

The feed must be an ingress to the API, not a path around it.
[Per-call validation against role ∩ step](design.md#where-it-is-enforced) is the enforcement
point for every item mutation; an action endpoint that performed effects on its own authority would
be a hole straight through it — and the article, posted by an agent, is the last thing that should
carry authority.

**Three things, kept separate:**

| | Governed by | Question it answers |
|---|---|---|
| **Visibility** | [read scope](base-engineering.md#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped) | may this principal see the article at all |
| **Routing** | `audience` | whose feed does it rank into |
| **Actionability** | per-action grant | may this principal take *this* action |

An article addressed to one role is not hidden from others, and seeing it implies nothing about
acting on it. Rendering should match capability — a button that fails trains people to distrust the
surface — but that is presentation; **the server check is the enforcement, never the reverse**, the
same relationship as [guard versus the diff at the step boundary](design.md#the-capability-vocabulary).

### The confused-deputy hazard

The feed carries messages **from low-trust agents to high-trust humans, with buttons attached.** A
`publish` step that cannot merge posts *"Ready to merge — click here"*; an admin clicks; the merge
happens with the admin's authority. The step has laundered a capability through a human.

The naive fix — an article may only offer actions its poster could take itself — **breaks
escalation**, which is the surface's entire purpose. A low-trust step surfacing a decision upward is
the design working correctly. So the mitigations are elsewhere:

- **`source` is stamped at the boundary, never claimed.**
- **Provenance appears in the confirm dialog**, not only on the card. Consent is meaningful only if
  the human knows who is asking.
- **The completeness rule:**

> **An action may be fired from a card only if the card carries everything needed to decide.
> Otherwise the card navigates.**

That is why a `choice` answer is fine inline — question, options, and recommendation are right there
— and why *merge* is not: a merge cannot be decided without reading the diff, so the card's job is
to take you to it. A sharper test than "is it dangerous", and it explains cases rather than
enumerating them.

### Four rules that close the remaining gaps

- **`answerable_by` comes from policy, not from the step.** Otherwise a step launders authority by
  declaring its own question trivial — mark a design-authority decision routine and any contributor
  may answer it. The step declares the question's *kind*; policy maps kind → required role, and that
  mapping lives in companion-repo config
  [where the agents it constrains cannot reach it](design.md#the-capability-vocabulary).
- **The action allow-list is a vocabulary, not a grant.** "It is allow-listed" must never read as
  "it is permitted" — the operation's own capability requirement is what is checked. Worth stating
  outright, because the name invites the opposite reading.
- **Removing a role is checked, never silent.** An `audience` naming a removed role degrades to
  everyone-in-read-scope with a fault raised; a *question* requiring one can never be answered,
  which is a stall wearing a config change as a disguise. No new machinery is needed: the design
  already promises Reactor can [check statically that a role is capable of completing a
  flow](design.md#where-it-is-enforced), and removal runs that check in reverse — enumerating
  every open question and step assignment the removal would strand. Refuse, or flag for admin
  review, the same posture as [changed gate metric
  semantics](design.md#gate-execution--reactors-half).
- **One role must always exist, with a live principal behind it.** Escalation needs a destination
  that cannot be absent — an escalation with nowhere to go is
  [a wait on something that will never arrive](design.md#reliability--never-stall-never-spin),
  and it is discovered at exactly the moment nothing can be done about it. So Reactor refuses a
  configuration that removes the last administrating role or leaves it with no principal, **checked
  at config load rather than at escalation time**. This is the bootstrap floor of the whole
  authority model, not a feed concern: a deployment that cannot reach a human who may change its
  roles is a locked room with the key inside.

Every action taken is recorded with principal, article key, source, and operation — the same habit
as [every reclamation is recorded](design.md#every-exclusion-is-held-by-a-process-never-by-a-flag).

## Questions with deadlines

A question is raised in one of two modes: **pinned** — *"I am blocked until you answer"* — or
**defaulted** — *"here are N answers, I recommend X; if you do not answer by Y, I take X."* The
second is the one that can quietly remove the human, so it carries the structure.

### Two clocks that are easy to confuse

> **An article's `expires_at` never decides anything.** The decision deadline lives on the question
> annotation, and Reactor fires it from durable state.

The article is a projection; its expiry is feed hygiene, and if the question is still parked the
[reconcile pass](#two-article-classes) brings it back. Driving a decision from an article's lifetime
would mean wiping the feed changed what the fleet decided, which
[feed-held state is an optimization, never authority](#feed-held-state-is-an-optimization-never-authority)
forbids outright.

### Policy maps the kind; the step declares only what it is asking

> **A question's *kind* determines both who may answer it and how long they have.** The step
> declares the kind. It sets neither.

`answerable_by` from policy is what stops a step laundering authority by calling a design decision
routine. The window comes from the same place for the same reason: **a step choosing its own
deadline is choosing how long its supervisor gets**, and a floor does not fix that, because a step
can always propose exactly the floor. A step that needs a faster answer says so by declaring a kind
that has one, and the kinds are policy-defined — in companion-repo config,
[where the agents they constrain cannot reach them](design.md#the-capability-vocabulary).

**The preference window must be shorter than the answer window.** A question
[addressed to a named principal](#audience-and-tags) sits only in their feed until the preference
lapses, so a preference outliving the deadline means the role never got a chance. Reactor rejects
the pairing — the same class of static check as
[refusing an unsatisfiable exclusion set up front](design.md#exclusions-are-declared-and-waiting-for-one-is-not-work).

### The window elapsing escalates before it decides

A window running out does not go straight to the default. It first widens the audience up the trust
ladder — each rung with its own window — until it reaches the role
[guaranteed to have a live principal](#four-rules-that-close-the-remaining-gaps). Only an exhausted
ladder defaults, and the full sequence is
[waiting on a person](design.md#waiting-on-a-person).

**Every question that can carry a defensible recommendation should carry one.** A question with no
default waits until a human answers it, which converts the fleet's throughput into one person's
response time — correct where an answer is genuinely required, and expensive everywhere else.

### What firing looks like

When the ladder is exhausted, Reactor:

1. Writes the **answer annotation** — `selection = <the recommendation>`, `author = system`,
   `arrival_path = default-fired`, plus the **delivery accounting**: how long the question was
   deliverable, which rungs of the ladder it climbed, and that each window elapsed.
2. Clears the park, so the resolver re-scans and the step resumes from its checkpoint plus the
   answer.
3. Records the question as **`defaulted`, never `answered`.**

Point 3 is not bookkeeping. *"The human chose the recommendation"* and *"nobody looked"* produce an
identical selection and must stay distinguishable forever, because the second is the ground-truth
loss signal the [whole ranking model calibrates against](#collecting-the-ground-truth-without-spending-what-it-saves).

### The clock runs on delivery, not on creation

A deadline on a person is only fair if the question reached them. The test is the one the design
already uses for [infrastructure versus process failure](design.md#infrastructure-failures-and-process-failures-are-different-things)
— *was the work ever evaluated?* — not whether the human engaged:

> **Delivered = at least one principal holding the required role could see the question, through a
> healthy surface.** The clock runs while that holds and pauses while it does not.

- **A read receipt is not delivery.** A human who never opens the detail view never "reads" it, so
  the clock would never start and the defaulted mode would silently become the pinned mode.
- **A human being absent is not a delivery failure.** That is precisely the case the defaulted mode
  exists for. Only the system's own failure to present buys time back.
- **The pause conditions are ones already detected**: the required role has no live principal, the
  article is mis-routed to an unregistered role, or every configured surface is degraded. Those are
  exactly the [faults the degraded-path rule already raises](#a-degraded-path-is-never-a-silent-path)
  — one list, two uses.
- **Per surface, not all surfaces.** If the feed is up and the code host is down, the question is
  still in front of people who can answer; the clock runs. It pauses only when *every* surface is
  failing, or degrading one mirror becomes a way to stall the fleet.
- **Pause and accumulate, never restart.** A six-hour outage inside a 48-hour window leaves 48 hours
  of real availability. Restarting would let a flapping surface extend the window forever.

**Chronic non-delivery converts a defaulted question into a pinned one** rather than firing it. If
delivery has been broken long enough that the window cannot be honoured, auto-deciding is the wrong
move — [default to the one that stops](design.md#infrastructure-failures-and-process-failures-are-different-things)
— and it makes the outage loud instead of hiding it inside a decision nobody made.

### Windows are learnable; authority is not

Policy sets a **range** per kind and calibration moves the window within it, against the same
objective and the same loss signal as the ranker. One line is absolute:

> **Timing is learnable. Authority is not.** Calibration may move the answer window. It may never
> touch `answerable_by`.

- **Asymmetric evidence.** Shortening a window means more decisions made without a human, so it
  demands materially more evidence than lengthening one — the same asymmetry as
  [defaulting to the failure that stops](design.md#infrastructure-failures-and-process-failures-are-different-things).
- **Never below the deployment floor**, whatever the data says.
- **Recorded and visible**, per deployment rather than global — different fleets have different
  humans.

**The best thing calibration can discover is not a better window but a question that should not be
asked.** A kind whose defaults are near-always accepted on review is ceremony, and retiring it saves
more attention than any amount of tuning. But the system **proposes** that and never decides it:
silently ceasing to ask is removing the human by inference, which is the one move this design exists
to prevent. It proposes it, of course, as an article.

## Wire format

A single JSON object, `flow:feed-article-v1`, carried in the
[BASE layer wire module](design.md#seams-are-process-boundaries--by-design-not-by-accident):

```jsonc
{
  "schema_version": 1,
  "key": "gate:build-time",
  "source": { "component": "gate", "name": "build-time" },
  "title": "Build time regressed 18% (42.7s → 50.4s)",
  "description": "`build ./...` crossed the ratchet baseline …",
  "media": [
    { "kind": "link", "label": "Trend chart", "url": "https://…/gates/build-time" },
    { "kind": "item", "label": "Suspect commit", "reference": "T0481" }
  ],
  "actions": [
    { "id": "open-gate", "label": "Open gate", "kind": "navigate", "primary": true,
      "navigate": { "target": "gate", "reference": "build-time" } },
    { "id": "file-bug", "label": "File bug", "kind": "operation", "after": "resolve",
      "explain": "Files a `perf`-tagged bug linked to this gate and assigns it to you.",
      "operation": { "name": "create-bug", "parameters": { "tag": "perf" } } },
    { "id": "rerun", "label": "Re-run full CI", "kind": "operation", "confirm": true,
      "explain": "Triggers the full paid CI suite (~12 min, billed). Safe but not free.",
      "operation": { "name": "run-gate", "parameters": { "gate": "build-time", "full": "true" } } }
  ],
  "impact_hours": 8,
  "attention_cost": "2m",
  "tags": ["topic:perf", "area:gates"]
}
```

The [gate output envelope](base-engineering.md#gate-output-envelope) gains an optional `articles`
array so a gate can post in the stdout it already emits:

```jsonc
// flow:gate-output-v1 (extended, additive)
{
  "schema_version": 1,
  "metrics": { "build_seconds": 50.4 },
  "articles": [ { /* flow:feed-article-v1; schema_version optional when nested */ } ]
}
```

## Emission channels

Four routes, one envelope:

1. **Flow API post.** The natural path for steps, crossing the same seam
   [every item mutation crosses](design.md#seams-are-process-boundaries--by-design-not-by-accident),
   so the post is authenticated and attributed like any other call. Idempotent on `key`.
2. **Gate output.** The `articles[]` field above, ingested when Reactor parses gate stdout — no
   extra round trip. A gate's `key` defaults to `gate:<name>` so supersede and recovery work without
   the gate thinking about it.
3. **MCP `post_article` tool.** For components that speak MCP rather than the flow API. Subject to
   the [MCP grant rules](design.md#the-capability-vocabulary) like any other mounted tool —
   allowlisted against a pinned server, and posting is a **write**.
4. **REST sink `POST /api/feed`.** The authoritative write path the other three converge on,
   authenticated as a principal. The server stamps `created_at`/`expires_at` and returns the stored
   article.

## Reactor side

### Store

- The stored article is the wire article plus server-owned fields: internal id, `created_at`,
  `expires_at`, `surfaced_at`, `read_at`, observed time-to-action, and which actions were taken.
  **There is no stored ranking state.**
- **One live state.** An article is present or it is gone. Rank is computed per read and the fold is
  per reader, so "decayed" is a *position*, not a state something is flipped into — and there is no
  archive tab, state, or endpoint.
- Keyed by `key`; supersede overwrites in place with the freshen rule.
- Lives in [LedgerStore](design.md#ledgerstore--per-server-active-state) with the rest of the
  hot, per-server active state — replacing the bare "notifications" entry there.
- **Nothing sticks forever, and the exit rule follows from the model.** An article whose regret has
  been effectively zero for a bounded window is **removed**, not kept below a fold: if nothing will
  ever make it worth a human's minute, it is not worth keeping. That is the same judgment
  [a river, not a ledger](#goal) already commits to, now with a reason behind it rather than a
  threshold. A global count cap remains as a backstop, evicting lowest-regret first.
- **The sweep does not rank.** It removes zero-regret articles and past-`expires_at` ones, and
  stamps nothing the read path needs. Its interval is deployment config, its value and source logged
  at startup, every removal logged with the article key and reason.
- The sweep is [a watched child like anything else](design.md#nothing-runs-unwatched) — no
  unbounded waits, no work in Reactor's own address space that a deadline cannot bound.

### Feed API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/feed` | **server-ranked** by regret per minute, cut at the caller's attention budget (`?budget=30m`, default from their profile); optional `?role=` and `?tag=` filters, intersected with the caller's read scope |
| `GET` | `/api/feed/more` | the next slice past the budget line, for a reader who has time — same ranking, no separate population |
| `POST` | `/api/feed` | ingest one article — first post creates, re-post of a `key` supersedes |
| `PATCH` | `/api/feed/{key}` | partial update without a full re-post |
| `POST` | `/api/feed/{id}/read` | mark read |
| `POST` | `/api/feed/{id}/dismiss` | user dismiss → removed (reason logged). **Never answers a question.** |
| `POST` | `/api/feed/{id}/action` | take a CTA: `{action_id, choice?, text?, confirmed?}`. **Checked against the caller's authority for the underlying operation**; rejects an unconfirmed action needing confirmation; performs the effect, applies `after`, and writes any durable record the action implies |
| `POST` | `/api/feed/{key}/resolve` | creator retraction → removed |

Ranking happens server-side so every client sees one ordering.

### Feed tab (UI)

**One linear list — no tabs inside the feed.** Ranked by regret per minute, cut at the reader's
attention budget.

- **The fold is the budget line.** A single end-of-feed *"Show more"* extends past it — one
  population, one ordering, no second bucket. Dismissed, resolved, and expired articles are gone,
  not tucked away.
- **The budget is visible and adjustable.** The reader can say *"I have ten minutes"* and the list
  shortens honestly rather than being scrolled past. Showing a running "≈24 min to clear" is the
  point of the whole model made legible.
- **Empty state.** When nothing above the line is worth a minute, a calm resting state — *"All
  caught up — nothing needs your attention"* — with "show more" still available beneath it.
- **Filtering, not tabs.** Text search and facet filters (audience role, namespaced tag chips)
  refine the one list in place.
- **Selecting an article opens a detail view** beside the list, showing the full description, all
  media, every action with its `explain`, and source/lifecycle metadata.

Per-article card: source attribution, title, time-ago, tags, an **estimated time to dispose of it**,
and *why it ranks where it does* — "blocking 11 items" or "decides itself in 40 minutes" — because a
rank a reader cannot account for is one they will stop trusting. Primary CTA prominent with
alternatives in an overflow; dismiss always present. Choice articles
render the option set inline **with a free-text field unless the question is `closed_form`**.
Destructive actions take caution styling; confirm-gated actions pop the dialog described above.
First attachment inline, remainder behind a disclosure.

## Mapping the notification inbox onto the feed

Every notification creator and remover becomes an article, with dedup made explicit:

| Notification | Becomes |
|---|---|
| `gate-failure` | `key="gate:<name>"`, `source{gate}`, High; accrual carries it — a red trunk blocks every landing, so it rises on its own; actions open-gate / create-bug |
| gate recovery | resolve on `gate:<name>` |
| `inspection-concern` | `key="item:<id>:concern"`; actions open-item / create-task |
| `closure-review` | `key="item:<id>:closure"`, Low impact, no blocked work — sinks on its own |
| `suggestion` | `key="item:<id>:suggestion"`; actions open-item / create-task |
| `verify-result` | `key="agent:<name>:verify"`; pass → Low and obsolete quickly, fail → High |
| `work-summary` | Low impact, purely informational — regret reaches zero and it leaves |
| `work-stalled` / `data-git-failure` | Critical, and blocking everything — stays at the top until resolved without needing a pin |
| item question | article with a `choice` action; the selection writes the answer annotation |
| auto-archive-prior dedup | explicit `key` supersede |

**The item-question case is the interesting unification**, and it is simpler here than in the
original proposal. There, answering had to *call back* into a component that no longer existed — a
gate is one-shot and a step has exited. Under
[steps dispatch themselves](base-engineering.md#step-resolution--steps-dispatch-themselves) and
context assembled at dispatch, nothing is delivered at all:

> The choice action writes the answer annotation → the park clears → the resolver re-scans → the
> step's `check` reads checkpoint + answer from durable state.

No callback envelope, no at-least-once delivery contract, no durable action queue. The same answer
covers gates: the action writes durable state and the next run reads it.

## Open questions

The design questions are settled. What remains is configuration a deployment supplies, and one
dependency outside this document.

1. **The seed set of question kinds.** Each maps to a required role and an answer-window range, and
   they are companion-repo config rather than contract — but a starting set has to exist, and it is
   what a project will get wrong first if it is not given good defaults, since a kind it invents
   inherits neither a sensible role nor a sensible window.
2. **The hours-per-currency rate** used to fold spend at risk into `impact_hours` for ordering. A
   deployment number with no defensible default; a deployment that leaves it unset simply ranks on
   hours and shows money alongside.
3. **The evidence threshold** at which an estimated remaining-work figure earns full weight. Too
   low and a new item kind swings the feed on two samples; too high and the estimated half never
   contributes.

## Why this shape

- **Open extension point, not a closed enum.** Any component posts via one envelope; Reactor never
  branches on component type. A new surface needs zero Reactor code changes — it registers as data —
  yet identifiers stay in a controlled vocabulary, so the feed does not decay into free-text soup.
- **Regret per minute replaces manual triage.** The feed self-curates against a stated objective —
  minimize the cost of decisions not made — so blockers rise as work piles behind them, deadlines
  climb as they approach, and information leaves when ignoring it costs nothing. No human clears a
  backlog, and no emitter has to guess its own position in someone else's list.
- **Calls to action are data, not frontend conditionals.** The creator declares what the user can
  do; the UI renders it generically.
- **Same contract boundary as gates.** A stable wire format is the contract; any package is
  convenience; all stateful machinery is Reactor's.
- **The feed holds no authority and no history**, so it can be wiped without loss and can never
  disagree with the record.

## Migration path

1. Land `flow:feed-article-v1` in the BASE layer wire module.
2. Add the `articles[]` field to `flow:gate-output-v1` (additive).
3. Add the flow API post call; backends without a feed degrade gracefully.
4. Build the Reactor side: store, `/api/feed` endpoints, ranker, sweep, MCP tool, Feed tab — then
   route every existing notification call site through it.
5. Add the summary section to [design.md](design.md), linking here for the full schema — the same
   relationship the gate manifest has between base-engineering.md and design.md.

No changes required to existing artifacts or flows — the feed is additive, exactly like gates.
