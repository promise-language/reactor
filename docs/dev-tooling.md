# Promise-based dev tooling

> **This document defines how project dev tooling is built and run** once the tools are written in
> Promise instead of Go — and why most of the machinery a Go blueprint needs stops being necessary.
> It is scaffolding rather than architecture: it exists because the Go toolchain forced a
> workaround, and it is expected to be deleted rather than maintained. The earlier Go blueprint is
> prior art — nothing here is ported from it or compatible with it.
>
> This is general, domain-agnostic tooling: it belongs to any project adopting BASE, not to Reactor.
> See [base-engineering.md](base-engineering.md) for how it fits alongside the rest of the BASE
> layer.
>
> **This model is load-bearing for gates, not just convenient.** A gate must be built from the
> commit it measures, so it is built or run from source in the worktree on every execution — see
> [base-engineering.md](base-engineering.md#the-principle). Flows are unaffected: they ship as
> prebuilt binaries and never compile in the worktree.

## The Go blueprint is scaffolding, not design

**It is a workaround stacked on a workaround.** Neither layer is something anyone wanted; both are
concessions to a toolchain.

**Layer one — why a language at all.** Project dev tools have to work identically on Linux, macOS,
and Windows. Shell scripts proved impossible to maintain consistently across three OSes, so the
tools had to be written in a real, cross-platform language. Go was that choice.

**Layer two — why the build apparatus.** Having picked Go, two of its properties then forced
everything else:

- **`go run` is slow enough to hurt per-invocation.** Dev tools run constantly — a pre-commit hook,
  a guard hook on every agent tool call, a gate. Paying a compile on every invocation is not
  viable, so tools get compiled ahead of time into `bin/`.
- **A `go run` binary executes from a temp directory.** `os.Args[0]` and `os.Executable()` point
  into a temp build cache, so a tool cannot locate its own source, and therefore cannot locate the
  repo root relative to itself. The blueprint works around this by having `./make` pin the working
  directory (`go run -C <repo>/tools/build`), which is how the meta-builder learns the absolute
  repo root at all.

Add the requirement that every tool reuse a common library instead of reinventing repo-root
detection, platform dispatch, and subprocess handling, and you arrive at what the blueprint is: a
meta-builder, a `bin/` staging area, committed `./make` / `make.cmd` trampolines, and a source-hash
stamp in each binary so a stale one refuses to run.

**Promise removes both layers.** It is natively cross-platform, which answers layer one. And
`promise run` over a project directory plus ordinary modules answers layer two — direct execution
and shared-library reuse, the two things Go could not give simultaneously. If the requirements below
are met, the entire apparatus — `bin/`, the meta-builder, the staleness stamps, the trampolines, and
the "your tools are stale, re-run `./make`" failure mode — does not get ported. It stops existing.

## The Promise model

A tool is a directory. You run it:

```sh
promise run tools/format -- --check
promise run tools/gate -- list --json
```

`promise run` already accepts a project directory as well as a single file, so the shape exists
today. What changes is that this becomes the *only* mechanism — there is no build step, no `bin/`,
and no staleness concept, because there is no artifact to go stale.

Bootstrapping collapses to one prerequisite: `promise` on PATH. Compare the Go blueprint, which
needs a Go toolchain **and** a committed shell trampoline **and** a successful `./make` before any
tool can run.

### Why a directory per tool, and not a file per tool

One `.pr` file per tool would be lighter still, but the module system rules it out — and not for the
obvious reason:

- **A file inside a project is compiled *as* that project.** `promise build main.pr` run inside a
  directory with a `promise.toml` detects the enclosing manifest and builds the **whole project**,
  naming the binary after `[module].name` rather than the file. So `tools/*.pr` sharing one project
  manifest would put every tool in one compilation unit, with as many colliding `main()`s as there
  are tools.
- **A sourced import needs a manifest either way.** A local import (`use common "./..."`) resolves
  relative to the importing *project's* module root, so there must be one. A remote import
  (`use common "github.com/..."`) must be pinned in the importing project's `[require]` section, so
  it needs a manifest too. A URL import does not escape the requirement.

Which leaves the actual rule: **a single-file tool can only depend on the catalog.** The moment a
tool needs a shared local library, it needs its own manifest — so each tool is a directory with a
`promise.toml`, and the shared library is a sibling module reached by relative path:

```
tools/
  common/     promise.toml   — shared library (repo-root detection, platform dispatch, exec helpers)
  gate/       promise.toml   — use common "../common";
  format/     promise.toml
  release/    promise.toml
```

**Verified against the compiler — this works today**, with three properties the tooling model needs:

- `use common "../common";` resolves correctly from a sibling tool directory. Parent-relative local
  imports are supported (`IsLocalPath` accepts `../`, and the module docs use `"../shared/auth"` as
  a worked example).
- **The library directory must carry its own `promise.toml`.** Without one the build fails with
  `error loading module '../common': ... no such file or directory`. Both the tool and the library
  are modules; only *organizational* subdirectories may go manifest-less.
- **Resolution is relative to the importing module root, not the working directory.** Running the
  same tool by absolute path from `/` produces identical results, so hooks and CI invoking a tool
  from anywhere behave the same.
- **A change to the shared library invalidates each tool's cached binary.** Editing `common.pr`
  turns the next `promise run tools/gate` into a cache MISS and picks up the new code — so tools
  cannot be silently served stale, which is the failure mode the source-hash stamps existed to
  prevent.

Each tool is its own module, so each gets its own `main()`. Nothing here needs a platform change.

**Unless the shared library becomes a catalog module.** If the BASE tool library ships in the
community catalog, `use basetools;` needs no path, no pin, and no manifest — and single-file tools
become possible after all. That is worth wanting for a reason beyond brevity: an adopting project
would get the whole BASE tooling layer with one import rather than by vendoring a directory, which
is the difference between a reusable layer and a copied one. The constraint to check is that catalog
modules may only depend on other catalog modules.

This is a real fork, not a detail: **directory-per-tool works today; catalog-published-library is
better if the library is meant to be reused across projects** — which, per
[base-engineering.md](base-engineering.md#what-lives-where), it is.

### What this requires from the platform

Checked against the compiler as it stands. One requirement is already met, one is partly met, and
two are open.

**1. Compile caching keyed on source content — ✅ already implemented.** `promise run` caches the
compiled binary under `~/.promise/cache/build/`, keyed by source, compiler hash, std hash, target,
mode, embeds, and local deps; project-mode keys cover the whole project directory, so a sibling
edit correctly invalidates. A cache hit skips compilation and executes directly, and
`PROMISE_CACHE_DEBUG` reports hit/miss with the key inputs. This was the load-bearing requirement,
and it exists.

What still needs confirming is the *workload*, not the mechanism: the latency-critical case is the
**guard hook, which runs on every single agent tool call**, and the cold case is a fresh ephemeral
arena where the cache starts empty. The first is a steady-state measurement; the second is
arena-provisioning work (ship or mount a warm cache).

**2. A tool must be able to find its own source directory — ❌ open.** This is the exact thing that
defeated `go run`. A tool needs to answer two *different* questions:

- *Where do I live?* — to locate the repo root, sibling tools, and committed data files, regardless
  of where it was invoked from.
- *Where was I invoked?* — to resolve user-supplied relative paths, which must behave exactly as
  they would for a compiled binary.

Conflating these is what produces the temp-directory bug, so both need to be available and
distinct. Either `promise run` exports the tool's source directory into the environment, or the
language exposes it as a compile-time constant. Given Promise's "no macros, all code visible in the
source file" stance, a `std` accessor or a meta annotation fits the language better than anything
macro-shaped — but the design call belongs to the language, not to this doc.

**3. Argument passing — ❌ open, and `--` does not help.** Confirmed by trying it:

```
$ promise run tools/verify --wasm extra
error reading extra: open extra: no such file or directory
$ promise run tools/verify -- --wasm extra
error reading extra: open extra: no such file or directory
```

`promise run` passes **no arguments at all** to the tool — it executes the compiled binary bare —
and its argument parser treats any unrecognized non-flag token as the *source path*, so trailing
arguments are misread as files to build. `--` is not a supported separator.

Every example in this doc — `promise run tools/gate -- list --json` — depends on this being solved.
A dev tool that cannot take arguments is not a dev tool: `verify --wasm`, `gate list --json`, and a
guard hook receiving its payload all need it. Whether the separator is `--` or everything after the
path passes through is a CLI design call; that *something* passes through is a hard requirement.

**4. Transparent process semantics — ⚠️ partly met.** Dev tools run as git hooks, as gates whose
stdout is parsed as a JSON envelope, and as CI steps. Today `promise run` wires stdin, stdout, and
stderr straight through to the child and propagates its exit code — so the stdio and status halves
are correct, and a gate's stdout stays exclusively the gate's as long as compiler diagnostics go to
stderr.

The gap is **signals**. `promise run` *spawns* the binary and waits, staying resident as its parent,
and forwards no signals to it; on Windows it deliberately places the child in a new process group,
which detaches it from console events. So a timeout kill aimed at `promise run` does not reach the
tool. That is exactly the supervision property Reactor depends on when it kills a hung gate — see
[base-engineering.md](base-engineering.md#language). Either `promise run` should `exec` the cached
binary in place (replacing itself, so there is no parent to kill) or it must forward signals
faithfully. Exec-in-place is the cleaner answer and matches the no-launcher rule flows already rely
on.

## What survives the change

The *conventions* survive; the build machinery does not. Nothing is ported — the Go blueprint is
prior art, and what follows is a list of ideas worth keeping, not artifacts worth carrying.

| Go blueprint concept | Under Promise |
|---|---|
| `./make` → compile all tools to `bin/` | **gone** — `promise run tools/<name>` |
| Source-hash staleness check | **gone** — nothing is pre-built, so nothing is stale |
| `bin/` as a gitignored artifact dir | **gone**, or a directory of thin shims for ergonomics |
| One tool = one directory under `tools/` | kept — and now *required*, see above |
| Shared common library across tools | kept — a sibling module (`use common "../common"`), or a catalog module |
| `bin/verify` as the commit gate | **superseded** — verify is [derived from the gate manifest](base-engineering.md#verify-is-derived-not-declared) and gates `push:origin`, not commit; no project authors it |
| Pre-commit hook | kept — it invokes `promise run`, and it *reports* rather than refuses for whole-tree properties ([invariant 2](base-engineering.md#invariants-and-properties-are-enforced-differently)) |
| Guard hook | **relocated** — a grant enforcer is authority, so its rules come from the companion repo and the arena applies them ([Bounds are authority, not tooling](base-engineering.md#bounds-are-authority-not-tooling)) |
| Ratcheted `.baselines.json` | kept — unrelated to how tools are built |

Optional ergonomic shim: keep `bin/gate` as a two-line script that execs `promise run
tools/gate -- "$@"`, so muscle memory, hook configs, and CI invocations don't change. Unlike the
Go `bin/`, that shim is committed text, not a build artifact.

## Migration

Per-tool, not big-bang. A project can hold both models simultaneously: the Go meta-builder keeps
building the Go tools it already has, while new tools land as Promise directories invoked through
`promise run`. A tool moves when someone rewrites it; the hooks and CI steps that call it change one
line. `./make` and the meta-builder are deleted only when the last Go tool is gone.

For Reactor specifically, the existing Go tooling stays as-is until the Promise path lands — see
[design.md](design.md#language).

## Open questions

- **Where does the shared tool library live** — a sibling module under `tools/`, or a community
  catalog module? **Sibling-by-relative-path is verified working**, so it is the safe default. The
  catalog option is what would make the BASE tooling layer reusable by import rather than by
  vendoring, and is what unlocks single-file tools; the constraint to check is the self-containment
  rule (catalog modules may depend only on catalog modules) against what the library actually needs
  — `os`, `io`, and `path` are all catalog, so this looks feasible.
- **Does `promise run <dir>` behave identically to `promise build <dir>` + exec** with respect to
  enclosing-project detection, working directory, and argument passing?
