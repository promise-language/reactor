# Reactor

OSS task-orchestration server and SDK — the successor to the private `tracker`
project. Reactor manages a backlog of items across a fleet of runners/arenas,
runs **flows** (flow-based binaries) to resolve them, and enforces continuous
quality with periodic **gates**.

**Status:** early bootstrap (M0). The architecture, the four operating
scenarios, the pluggable persistence split (`ItemStore` / `ConfigStore` /
`LedgerStore` + the flow execution layer), and the tracker → Reactor transition
are documented in **[docs/design.md](docs/design.md)**.

## Build

Reactor uses the [forge](https://github.com/promise-language/forge) dev-tooling
blueprint — one in-repo Go module that compiles every dev tool into `bin/`:

```sh
./make        # compile dev tools into bin/ (verify, guard, precommit, setup)
bin/verify    # the commit gate: format → vet → build → test
```

`bin/` is gitignored; tools are built on demand, never committed.

## License

Dual-licensed under either [Apache-2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT),
at your option. See [LICENSE](LICENSE).
