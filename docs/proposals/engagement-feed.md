# Proposal: the engagement feed

> **Status: draft.** Moved from `flow` and adapted to Reactor's design — the
> durability and authority rules below are settled; **[#3](#open-3--one-role-vocabulary-for-the-system)
> and [#4](#open-4--an-expiring-question-must-decide-not-vanish) are open** and marked inline.
>
> **Related:** [design.md](../design.md) · [base-engineering.md](../base-engineering.md)

## Goal

Define one **abstract, well-defined engagement surface** that any component in the system — a flow
step, a gate, an item question, an item concern, Reactor itself, or a future component nobody has
written yet — can post to without Reactor needing to know that component exists.

The core unit is a **feed article**: a durably-identified, self-describing, *decaying* call to
attention. The user reads a single ranked **feed** (the "Feed" tab of the Reactor UI), where
articles are ordered by a score derived from their priority and how long they have been aging
against their half-life. The user can **dismiss** an article, **take one of its calls to action**,
or **navigate** to whatever it references.

This is the mechanism behind the white paper's [inbox → feed](../../WHITEPAPER.md) direction:
*"a social-media-style stream the human engages with on their own schedule, while the resolution
loop keeps running underneath."* Human attention is the scarcest resource, and the system is
engineered to spend as little of it as possible.

**A river, not a ledger.** The feed is ephemeral by design, like a social timeline: an article is
either engaged with *now* or it flows away. There is no archive and no permanent read-history — you
cannot scroll back to what you read two years ago. Articles leave by decaying out (then, after a
bounded tail, deleted), or by being dismissed/resolved/expired (removed at once). **Nothing is
retained forever.** This shapes every storage and UI decision below: the system optimizes for "what
deserves attention now," not for recall.

The design rests on two properties. First, the article *kind* is open-ended: the component
identifier is a registry-backed value, not a closed enum, so an unforeseen component can post after
a data-level registration — no Reactor code change. Second, priority, decay, audience, and
calls-to-action are first-class, creator-controlled fields rather than Reactor-side conditionals.
Identifiers that would otherwise drift into free-text soup (component, audience, tags) are kept to
controlled vocabularies — stamped at the boundary and canonicalized on ingest (see
[Identifiers](#identifiers-controlled-vocabularies-not-free-text)).

### What this proposal adds

Mirroring the [gate contract's](../base-engineering.md#gate-output-envelope) split of concerns:

1. A **stable wire format** for a feed article (`flow:feed-article-v1`) — a single JSON envelope any
   language can emit, living in the
   [BASE layer wire module](../design.md#seams-are-process-boundaries--by-design-not-by-accident)
   alongside the flow↔Reactor types and the gate envelope.
2. **Emission channels** that route an article from a component to Reactor: a post call on the flow
   API, an `articles` array on the gate output envelope, an MCP `post_article` tool, and the REST
   sink `POST /api/feed`.
3. A **ranking contract** (priority × half-life decay) defined here so every deployment ranks
   identically.
4. **Reactor-side machinery** — the store, the ranker, the sweep, the feed API, and the Feed tab.

The contract is **the JSON, not any SDK**. A Promise convenience package may exist for typed
emitters, but an article that depends on that package's *existence* has broken the contract — the
same rule the [gate boundary](../design.md#language) already lives by, and the resolution of the
original proposal's open question 6.

## Feed-held state is an optimization, never authority

> **Feed-held state is an optimization, never authority.** Wipe the entire feed store and nothing is
> lost but attention.

This is deliberately the same sentence the design applies to arenas —
[*"arena-held state is an optimization, never authority: every fact a correctness decision rests on
must already have been streamed to the server or
committed"*](../design.md#a-host-that-is-merely-off-is-not-a-host-that-is-gone).
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
[every piece of persisted state](../design.md#every-exclusion-is-held-by-a-process-never-by-a-flag)
— **held** or **timed**, with no third form.

| Class | Identity | Lifetime | Examples |
|---|---|---|---|
| **Condition** | derived key | **held** — exists while the condition is asserted | gate X is red · item #481 is parked on a question · arena Y is absent |
| **Event** | per-occurrence key | **timed** — decays out | today's work summary · a run finished |

**A condition article's key is a pure function of the condition it projects.** That is the
[branch-naming rule](../base-engineering.md#branches-are-mechanical-and-there-is-exactly-one-per-claim)
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
[*is this unblocked?*](../design.md#an-edge-names-a-target-and-a-condition-never-a-version) is the
natural home. Controller-shaped — desired state versus actual — which is what makes "wipe the
feed" genuinely safe rather than merely survivable.

### Questions, answers, and history

A parked question is a condition, so it inherits all of the above: key derived from
`(item, question id)`, one article by construction, retracted when the answer lands.

The durable records live on the **item**, never in the feed:

| Record | Holds | Why it must be durable |
|---|---|---|
| **question** annotation | id, text, options, recommendation, mode, deadline, `answerable_by`, `addressed_to`, owning step | the resumed step reconstructs what was asked; [context is assembled from durable artifacts at dispatch](../base-engineering.md#context-is-assembled-never-accumulated) |
| **answer** annotation | question id, selection and/or free text, **author**, **arrival path**, timestamp | it steers autonomous work — a decision with an author |
| **checkpoint** | the step's partial work | so an unblocked step resumes rather than restarts |

Storage is composite, exactly like items: the human-readable question and answer are issue comments
(so a contributor reads and answers where they already work); the structured form is the
[private overlay](../design.md#itemstore--composite-identity-github--private-overlay) keyed by the
same id.

**Arrival path is not bookkeeping.** "A human picked the recommendation" and "the deadline fired and
took the recommendation" produce an identical selection and mean entirely different things. A record
that cannot distinguish them shows a human decision that never happened.

**Free text is the default; only Reactor may take it away.** A step that could say *answer only A or
B* would be the constrained party narrowing its supervisor's reply — the same self-authorizing shape
the design rejects when
[a flow declares its own grants](../design.md#what-a-flow-declares-and-what-is-declared-about-it).
A step proposes options; it may not restrict the reply. `closed_form` is legitimate in exactly one
case — when **Reactor itself** consumes the answer rather than handing it to an agent (a budget
approval, a "declare this arena lost"), where prose is ambiguous and the server acts on it directly.
Everything else is read by a step, and a step is an agent that can interpret *"none of these, do D,
because the second one breaks the WASM target"* — the answer you most want to be able to give and
the one a closed enum destroys.

**An answer is input, not authorization.** It feeds the work; it never feeds the bound. Effective
authority stays [role ∩ step](../design.md#authority-roles-steps-and-capabilities) regardless of
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

  // Ranking inputs.
  "priority":  50,                    // raw weight; higher = more important. Anchors: 12.5 / 25 / 50 / 100
  "half_life": "48h",                 // flow:duration; omitted = deployment default
  "pin":       false,                 // float above unpinned and never decay out; still expires

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
| `schema_version` | wire version. Unknown majors are **refused**; evolution within a major is **additive-only** — [the standing rule for every wire contract](../design.md#a-shared-module-is-not-a-shared-version). |
| `key` | durable identity, creator-namespaced. For a **condition** article it is a pure function of the condition. |
| `source` | who created it — stamped at the boundary, never claimed (see [Source](#source--who-created-it)). |
| `title` / `description` | card headline and markdown body. |
| `media` | ordered attachments; the first is primary. |
| `actions` | the calls to action (see [Action](#action--the-calls-to-action)). |
| `priority` | raw weight; the value *is* the base score before decay. |
| `half_life` | how fast priority decays. Omitted ≡ deployment default. |
| `pin` | float above all unpinned articles and never decay out. Does **not** exempt from hard expiry. |
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
3. **Registry, not enum; advisory, not behavioral.** The legal set of `component` values, audience
   roles, and tag namespaces lives in a **Reactor-owned registry** — seeded with the built-ins and
   extended by config/registration, not a code change. Reactor **never branches behavior** on any of
   them; they drive attribution, grouping, and filtering only. A typo or unregistered value degrades
   a chip or a filter — never logic — and is surfaced as "unregistered" rather than silently
   trusted.

### Source — who created it

```jsonc
{
  "component": "gate",          // registered id: flow | gate | item-question | item-concern | reactor | <registered>
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
// kind: "link" | "image" | "file" | "item" | "patch"
// reference: internal reference (item id, patch hash, …) when kind in {item, patch}
```

Bytes are **not** embedded in the article — the same rule as the gate output envelope. An
`image`/`file` attachment points at an external URL or at a Reactor-served blob uploaded separately;
the article carries the reference only.

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

### Priority and duration

`priority` is a raw numeric weight, not an enum: higher means more important, and the value *is* the
base score before decay. Any positive number is valid; named anchors on a 0–100 scale mean an
emitter rarely types a bare number:

| Anchor | Weight |
|---|---|
| Low | 12.5 |
| Medium | 25 |
| High | 50 |
| Critical | 100 |

The levels double each step — handy, because each level then buys exactly one extra half-life of
staying power. The absolute scale is arbitrary: only the ratios between levels, and of a level to
the decay floor, affect ordering. The numeric form drops the enum→weight lookup the ranker would
otherwise need and lets a creator sit *between* anchors when "higher than a normal high, below
critical" is the honest intent.

`half_life` is a `flow:duration` string (`"4h"`, `"36h"`, `"7d"`) to keep the wire language-neutral.

### Audience and tags

> **Open ([#3](#open-3--one-role-vocabulary-for-the-system)).** The role vocabulary below is the
> original proposal's, and it must not survive as written — the feed has to *consume* the system's
> role definition, not define a second one.

```jsonc
{ "role": "operator", "reference": "verifier-1" }   // omitted/empty = everyone
```

The mess in "who is this for" comes from per-person free strings. Targeting by **role** keeps the
vocabulary tiny and stable; an optional `reference` names a specific identity *within* a role.
"For me" is the filter `role == <the viewer's role>` matching the configured identity — there is no
place to type a raw username.

**Audience is routing, not authority.** It decides whose feed an article ranks into. It does not
decide who may see it (read scope does) and it does not decide who may act on it (see
[Authority](#authority-over-article-actions)).

**Tags** are advisory display/filter facets, each shaped `namespace:value` (`topic:perf`,
`area:build`, `severity:regression`). Two hard rules keep the tag set from rotting:

- **Tags never drive behavior** — only chips and filters. A typo costs a filter hit, nothing more.
- **Don't duplicate what `source`/`key` already say.** Component, item, gate, and agent are facets
  Reactor generates for free; tags carry only cross-cutting themes.

## Ranking — priority × half-life decay

The feed is sorted by a **decayed score** recomputed at read time. This is the one piece of math the
contract pins, so every deployment ranks identically:

```
half_life = article.half_life if set, else the deployment default
age       = now - created_at
score(a)  = a.priority * 2^(-age / half_life)

order: pinned articles first, then unpinned; within each group, score desc
```

**Scoring is identical for every article.** Pinning affects only the final ordering and removal,
never the score: pinned articles float into a group *above* all unpinned ones and sort against each
other by the same decayed score.

- An article with `half_life = 24h` loses half its weight every day: a `Critical` (100) posted 24h
  ago (→50) ties a fresh `High` (50) and still outranks a fresh `Medium` (25). Tune the half-life to
  encode "how long should this keep shouting."
- **`pin` floats and exempts from decay-out**, not from expiry. Use it for standing conditions that
  must stay up top and not silently age out.
- **Decay floor → drops below the fold.** When an *unpinned* score drops below a configurable floor,
  the article is marked `decayed` and collapses below the fold — out of the main list but reachable
  via an end-of-feed "show decayed" toggle until it falls past the decayed cap (lowest-ranked
  dropped first), then deleted. It is *not* moved to a separate tab; the feed stays one list.
- **Hard expiry applies to pinned articles too.** Optional `expires_at` deletes an article
  regardless of score or pin state.

> **Open ([#4](#open-4--an-expiring-question-must-decide-not-vanish)).** Deleting on expiry is right
> for an informational article and wrong for a question with a deadline, which must produce a
> recorded decision instead.

The floor and the anchor weights are deployment config held in
[ConfigStore](../design.md#configstore--the-deployment-owners-residual), not contract constants —
the same division of labour as gate ratchet caps.

## Durable identity, supersede, and resolve

`key` is creator-chosen and namespaced (`"gate:build-time"`, `"item:T0481:concern"`,
`"breaker:work-stalled"`), and **derived rather than chosen for condition articles**. It replaces an
inferred dedup tuple with an explicit, single field.

- **First post of a `key`** creates the article; Reactor stamps `created_at`.
- **Re-post of an existing `key`** *supersedes in place*: content, actions, and priority are
  replaced. A `freshen` flag chooses whether `created_at` resets (restart the decay clock — "this
  got worse again") or is preserved (keep aging — "same condition, updated details").
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

The feed must be an ingress to the API, not a path around it.
[Per-call validation against role ∩ step](../design.md#where-it-is-enforced) is the enforcement
point for every item mutation; an action endpoint that performed effects on its own authority would
be a hole straight through it — and the article, posted by an agent, is the last thing that should
carry authority.

**Three things, kept separate:**

| | Governed by | Question it answers |
|---|---|---|
| **Visibility** | [read scope](../base-engineering.md#5-a-change-writes-to-one-project-and-reads-only-what-it-was-scoped) | may this principal see the article at all |
| **Routing** | `audience` | whose feed does it rank into |
| **Actionability** | per-action grant | may this principal take *this* action |

An article addressed to one role is not hidden from others, and seeing it implies nothing about
acting on it. Rendering should match capability — a button that fails trains people to distrust the
surface — but that is presentation; **the server check is the enforcement, never the reverse**, the
same relationship as [guard versus the diff at the step boundary](../design.md#the-capability-vocabulary).

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

### Two rules that close the remaining gaps

- **`answerable_by` comes from policy, not from the step.** Otherwise a step launders authority by
  declaring its own question trivial — mark a design-authority decision routine and any contributor
  may answer it. The step declares the question's *kind*; policy maps kind → required role, and that
  mapping lives in companion-repo config
  [where the agents it constrains cannot reach it](../design.md#the-capability-vocabulary).
- **The action allow-list is a vocabulary, not a grant.** "It is allow-listed" must never read as
  "it is permitted" — the operation's own capability requirement is what is checked. Worth stating
  outright, because the name invites the opposite reading.

Every action taken is recorded with principal, article key, source, and operation — the same habit
as [every reclamation is recorded](../design.md#every-exclusion-is-held-by-a-process-never-by-a-flag).

## Wire format

A single JSON object, `flow:feed-article-v1`, carried in the
[BASE layer wire module](../design.md#seams-are-process-boundaries--by-design-not-by-accident):

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
  "priority": 50,
  "half_life": "48h",
  "tags": ["topic:perf", "area:gates"]
}
```

The [gate output envelope](../base-engineering.md#gate-output-envelope) gains an optional `articles`
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
   [every item mutation crosses](../design.md#seams-are-process-boundaries--by-design-not-by-accident),
   so the post is authenticated and attributed like any other call. Idempotent on `key`.
2. **Gate output.** The `articles[]` field above, ingested when Reactor parses gate stdout — no
   extra round trip. A gate's `key` defaults to `gate:<name>` so supersede and recovery work without
   the gate thinking about it.
3. **MCP `post_article` tool.** For components that speak MCP rather than the flow API. Subject to
   the [MCP grant rules](../design.md#the-capability-vocabulary) like any other mounted tool —
   allowlisted against a pinned server, and posting is a **write**.
4. **REST sink `POST /api/feed`.** The authoritative write path the other three converge on,
   authenticated as a principal. The server stamps `created_at`/`expires_at` and returns the stored
   article.

## Reactor side

### Store

- The stored article is the wire article plus server-owned fields: internal id, `created_at`,
  `expires_at`, `state` — **just `active` | `decayed`** — `read_at`, and which actions have been
  taken.
- **No archived state.** Every other exit — user **dismiss**, creator **resolve**, hard **expire** —
  removes the record; the exit reason is logged, not stored. There is no archive tab, state, or
  endpoint.
- Keyed by `key`; supersede overwrites in place with the freshen rule.
- Lives in [LedgerStore](../design.md#ledgerstore--per-server-active-state) with the rest of the
  hot, per-server active state — replacing the bare "notifications" entry there.
- **Nothing sticks forever — one fixed cap, enforced on decay-in.** At most *N* articles (default
  1000) may live under the fold. The cap is enforced only when a new article decays in: append, and
  if that pushes the count over, drop the lowest-scoring beyond the limit. No periodic tail scan.
- **No decay TTL on disk.** Decay is computed at read time from `created_at` + `half_life`. The only
  background job is a sweep that flips below-floor articles to `decayed` (enforcing the cap inline)
  and deletes past-`expires_at` articles. Its interval is deployment config, its value and source
  logged at startup, and every flip and delete logged with the article key and reason.
- The sweep is [a watched child like anything else](../design.md#nothing-runs-unwatched) — no
  unbounded waits, no work in Reactor's own address space that a deadline cannot bound.

### Feed API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/feed` | active articles, **server-ranked** (pinned first, then decayed score desc); optional `?role=` and `?tag=` filters, intersected with the caller's read scope |
| `GET` | `/api/feed/decayed` | the bounded decayed tail behind the "show decayed" toggle |
| `POST` | `/api/feed` | ingest one article — first post creates, re-post of a `key` supersedes |
| `PATCH` | `/api/feed/{key}` | partial update without a full re-post |
| `POST` | `/api/feed/{id}/read` | mark read |
| `POST` | `/api/feed/{id}/dismiss` | user dismiss → removed (reason logged). **Never answers a question.** |
| `POST` | `/api/feed/{id}/action` | take a CTA: `{action_id, choice?, text?, confirmed?}`. **Checked against the caller's authority for the underlying operation**; rejects an unconfirmed action needing confirmation; performs the effect, applies `after`, and writes any durable record the action implies |
| `POST` | `/api/feed/{key}/resolve` | creator retraction → removed |

Ranking happens server-side so every client sees one ordering.

### Feed tab (UI)

**One linear list — no tabs inside the feed.** Pinned articles first with a pin marker, then the
score-ranked remainder.

- **Decayed below the fold.** A single end-of-feed *"Show N decayed"* toggle — the only
  below-the-fold bucket. Dismissed, resolved, and expired articles are gone, not tucked away.
- **Empty state.** When nothing is above the floor, a calm resting state — *"All caught up — nothing
  needs your attention"* — with the decayed toggle still available beneath it.
- **Filtering, not tabs.** Text search and facet filters (audience role, namespaced tag chips)
  refine the one list in place.
- **Selecting an article opens a detail view** beside the list, showing the full description, all
  media, every action with its `explain`, and source/lifecycle metadata.

Per-article card: priority/decay indicator, pin marker, source attribution, title, time-ago, tags;
primary CTA prominent with alternatives in an overflow; dismiss always present. Choice articles
render the option set inline **with a free-text field unless the question is `closed_form`**.
Destructive actions take caution styling; confirm-gated actions pop the dialog described above.
First attachment inline, remainder behind a disclosure.

## Mapping the notification inbox onto the feed

Every notification creator and remover becomes an article, with dedup made explicit:

| Notification | Becomes |
|---|---|
| `gate-failure` | `key="gate:<name>"`, `source{gate}`, High, pinned; actions open-gate / create-bug |
| gate recovery | resolve on `gate:<name>` |
| `inspection-concern` | `key="item:<id>:concern"`; actions open-item / create-task |
| `closure-review` | `key="item:<id>:closure"`, Low, decays |
| `suggestion` | `key="item:<id>:suggestion"`; actions open-item / create-task |
| `verify-result` | `key="agent:<name>:verify"`; pass → Low + short half-life, fail → High |
| `work-summary` | Low, short half-life — decays out on its own |
| `work-stalled` / `data-git-failure` | pinned + Critical; removed only on resolve |
| item question | article with a `choice` action; the selection writes the answer annotation |
| auto-archive-prior dedup | explicit `key` supersede |

**The item-question case is the interesting unification**, and it is simpler here than in the
original proposal. There, answering had to *call back* into a component that no longer existed — a
gate is one-shot and a step has exited. Under
[steps dispatch themselves](../base-engineering.md#step-resolution--steps-dispatch-themselves) and
context assembled at dispatch, nothing is delivered at all:

> The choice action writes the answer annotation → the park clears → the resolver re-scans → the
> step's `check` reads checkpoint + answer from durable state.

No callback envelope, no at-least-once delivery contract, no durable action queue. The same answer
covers gates: the action writes durable state and the next run reads it.

## Open questions

### Open (#3) — one role vocabulary for the system

The original proposal seeds audience roles as `operator | reviewer | agent`, which is a **second**
role vocabulary alongside the [authority model's](../design.md#authority-roles-steps-and-capabilities)
project-defined roles. Two vocabularies naming the same principals is the
[mirrored-knowledge failure](../base-engineering.md#no-manual-gate-registration) that discovery
exists to prevent.

**The feed must consume the system's role definition, not define one.** What remains open is where
that single definition lives, how the feed references it, and what happens to an article whose
audience names a role that has since been removed.

### Open (#4) — an expiring question must decide, not vanish

`expires_at` currently deletes. For a question in the *defaulted* mode — *"here are N answers, I
recommend X; if you do not answer by Y, I take X"* — expiry must instead produce the recorded
default answer, authored by the system and citing the policy and the step's recommendation. Deleting
the article and leaving the step parked forever, or firing the default with no record, both violate
[nothing terminates into ambiguity](../design.md#nothing-runs-unwatched) at precisely the point
where a decision was made without a human.

Also open here: the deadline clock starting on **delivery** rather than creation, the deployment
floor on how short a step may propose making it, and whether policy may only narrow a question's
mode (defaulted → pinned) and never widen it.

### Carried over from the original proposal

1. **The user-facing noun.** `Brief` / `Post` / `Bulletin` / `Dispatch` / `Notice`. This document
   uses *article* generically and `flow:feed-article-v1` as the wire tag so the schema name survives
   whatever the unit is finally called.
2. **Priority anchors.** Numeric chosen (the value *is* the weight). Sub-question: should the wire
   also accept anchor names as string sugar (`"high"` → 50) for hand-authored JSON?
3. **Registry seeding.** The seed sets for the three registries, and whether an unregistered value
   is rejected at the sink or accepted-but-flagged. This document leans accepted-but-flagged so a
   new component is never blocked, with the typo cost bounded to a mislabeled chip.
4. **Decay curve.** Keep v1 to the single exponential half-life; revisit if a real case needs
   another shape. Note that a deadline-bearing question wants an *urgency* curve rather than a
   freshness one — `pin` within the final window is the cheap approximation, and it needs settling
   with #4.

*(Original open question 4 — callback delivery — is resolved above: there is no callback.
Original open question 6 — whether an SDK ships at all — is resolved by the wire being the
contract.)*

## Why this shape

- **Open extension point, not a closed enum.** Any component posts via one envelope; Reactor never
  branches on component type. A new surface needs zero Reactor code changes — it registers as data —
  yet identifiers stay in a controlled vocabulary, so the feed does not decay into free-text soup.
- **Priority + decay replace manual triage.** The feed self-curates: important things rank up, stale
  things age out, standing conditions stay loud. No human clears a backlog.
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
5. Add the summary section to [design.md](../design.md), linking here for the full schema — the same
   relationship the gate manifest has between base-engineering.md and design.md.

No changes required to existing artifacts or flows — the feed is additive, exactly like gates.
