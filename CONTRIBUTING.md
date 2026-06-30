# Contributing to Reactor

**Reactor** is part of the **Promise Lang** project, hosted in the
`promise-language` organization and maintained under Promise Lang LLC.

## Contributor License Agreement (CLA) required

Before any pull request can be merged, you must sign the **Promise Lang
Contributor License Agreement**. When you open your first pull request, the CLA
Assistant bot will post a link to sign. You only need to sign once — it covers
all future contributions across the project.

- **Individual contributors** sign the Individual CLA.
- **Contributors acting on behalf of an employer** also have their employer sign
  the Corporate CLA.

You retain copyright in your contribution; the CLA grants Promise Lang LLC the
rights it needs to administer, distribute, and sublicense it as part of the
project.

## Licensing of contributions

Unless you state otherwise, any contribution you intentionally submit for
inclusion is dual-licensed under the [Apache License 2.0](LICENSE-APACHE) and
the [MIT License](LICENSE-MIT), with no additional terms or conditions. This is
core, LLC-covered code: contributions must **not** introduce code under a
copyleft license (GPL, LGPL, AGPL, EUPL, or similar) or code of uncertain
provenance.

## How to contribute

Reactor is an OSS task-orchestration server and SDK — the successor to the
private `tracker` project (see the [README](README.md), and
[docs/design.md](docs/design.md) for the full architecture spec).

1. Open an issue describing the bug or feature, where practical, so the design
   can be discussed before you invest in a PR.
2. Keep changes within the existing architecture — the persistence split by
   concern (`ItemStore` / `ConfigStore` / `LedgerStore` plus the flow execution
   layer on top) and the guiding principle of *pushing domain logic to the
   project and keeping Reactor thin* are what make the model reliable. New
   surface should match the patterns already in the codebase.
3. Run the quality gate (`bin/verify`) and keep it green; add tests for new
   behavior.
4. Open a pull request and sign the CLA when prompted.
