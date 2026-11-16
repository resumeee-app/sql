# resumeee-storage-sql

Multi-database persistence backend implementing [`core/store.Backend`](https://github.com/resumeee-app/core) via GORM across SQLite (embedded pure-Go), PostgreSQL, and MySQL with TLS enforcement and DSN credential sanitization.

All domain rules, quota accounting, skill resolution, `MainRevision` bumping, `Branch.Epoch` fencing, and unified job queue scheduling live cleanly in `core/store`. This repository provides durable CRUD persistence, atomic transaction isolation (`BEGIN IMMEDIATE` for SQLite; `SERIALIZABLE` with retry for PostgreSQL/MySQL), schema migrations, and baseline bootstrapping returning `*store.Store`.

## Architecture

This repository is part of the **Resumeee** multi-module suite:

```
github.com/resumeee-app/
├── core                   # Pure domain entities, centralized store.Store engine, worker, storetest
├── resumeee-storage-sql   # Pure SQL store.Backend adapter (GORM SQLite / Postgres / MySQL)
├── resumeee-auth-firebase # Token verifier adapter (Firebase RS256 JWKS)
├── server                 # Production REST & MCP server and asynchronous worker pipeline
├── resumeee-web           # React 18 / Vite / TypeScript single-page application
├── resumeee-mcp           # Standalone Model Context Protocol (MCP) server
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

## License

AGPL-3.0
