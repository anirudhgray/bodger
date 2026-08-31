# User guide

**This guide is intentionally empty, and will stay that way until milestone 1 ships.**

There is nothing to document yet. `bodger` currently contains its architecture, decision records, and toolchain scaffolding — no application code, no commands, no interface a user could touch. See [Status](architecture.md#9-status).

Writing this guide now would mean documenting an interface that does not exist, against a design that is still allowed to change. The first version would be wrong before anyone read it, and a user guide that has been wrong once is not trusted again.

## What lands here, and when

**With milestone 1** (ledger core, CLI, and REST API) — the first content a user could act on:

- Installing and first run
- Creating accounts and categories
- Recording an expense, an income, a transfer, and a split
- Reading balances
- The CLI command reference
- The REST API reference

**With milestone 2** (web UI and authentication): getting the web UI running, the Docker Compose deployment, backing up your data, and setting up a login.

**Later milestones** add multi-currency and FX, reports and charts, import and export, budgets, and the MCP server — each documented in the same PR that ships it, per [`contributing.md`](contributing.md), not in a catch-up pass afterwards.

## In the meantime

- **What is this?** → [`README.md`](../README.md)
- **How does it work?** → [`architecture.md`](architecture.md)
- **What does it store, and what do the words mean?** → [`data-model.md`](data-model.md)
- **How do I build it?** → [`contributing.md`](contributing.md)
