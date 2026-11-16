# sql

Multi-database persistence backend implementing [`core/store.Backend`](https://github.com/resumeee-app/core) via GORM across SQLite (embedded pure-Go), PostgreSQL, and MySQL with TLS enforcement and DSN credential sanitization.

All domain rules, quota accounting, skill resolution, `MainRevision` bumping, `Branch.Epoch` fencing, and unified job queue scheduling live cleanly in `core/store`. This repository provides durable CRUD persistence, atomic transaction isolation (`BEGIN IMMEDIATE` for SQLite; `SERIALIZABLE` with retry for PostgreSQL/MySQL), schema migrations, and baseline bootstrapping returning `*store.Store`.

## Architecture

This repository is part of the **Resumeee** multi-module suite:

```
github.com/resumeee-app/
├── core                   # Pure domain entities, centralized store.Store engine, worker, storetest
├── sql                    # Pure SQL store.Backend adapter (GORM SQLite / Postgres / MySQL)
├── server                 # Production REST & MCP server and asynchronous worker pipeline
├── web                    # React 18 / Vite / TypeScript single-page applicatio
└── resumeee               # Flagship umbrella distribution repository
```

## Packages

- **`sql`**: Relational `store.Backend` implementation (`Backend`, `NewBackend`, `Open`, `OpenWithOptions`, `OpenStore`), schema migrations (`AutoMigrate`, `MigrateSchema`), connection pooling, dialect configuration, and baseline seeding (`SeedBaseline`, `SeedDefaultData`).

## Quality Gates

```bash
GOWORK=off make verify
```

Runs:
- `gofmt -l .`
- `go test -race -v ./...` (executes full `core/storetest` compliance suite against both SQLite memory and file-backed WAL databases, schema/index checks, rollback atomicity, and multi-instance file concurrency tests)
- `golangci-lint run ./...`

## License & Trademarks

### License
Resumeee is licensed under the **GNU Affero General Public License v3.0 or later** ([AGPL-3.0-or-later](LICENSE)).

You are free to use, modify, and host this software. Under the AGPL, any modified versions distributed or hosted over a network must make their complete source code freely available under the same license terms.

For commercial inquiries or custom closed-source licensing arrangements, contact Ruben Rivera.

### Contributor Agreement
All external contributions are welcomed under the terms of our [Contributor License Agreement (CLA)](CLA.md). See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

### Trademark Notice
"Resumeee" and the Resumeee branding and logo are trademarks of Ruben Rivera. The AGPLv3 license gives you rights to the software code, but does not grant rights to use the official name, logo, or trademarks for derivative products or publicity without prior written permission.
