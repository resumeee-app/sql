# resumeee-storage-sql

Multi-database persistence adapter implementing `domain.Store` using GORM, supporting SQLite (embedded pure-Go), PostgreSQL, and MySQL with TLS enforcement and DSN credential sanitization.

## Architecture

This repository is part of the **Resumeee** multi-module suite:

```
github.com/resumeee-app/
├── resumeee-core         # Pure domain entities, value types, validation rules, domain services
├── resumeee-storage-sql  # Multi-database persistence adapter (GORM SQLite / Postgres / MySQL)
├── resumeee-auth-firebase# Token verifier adapter (Firebase RS256 JWKS)
├── resumeee-api          # Production REST HTTP server and asynchronous worker pipeline
├── resumeee-web          # React 18 / Vite / TypeScript single-page application
├── resumeee-mcp          # Standalone Model Context Protocol (MCP) server
└── resumeee              # Flagship umbrella distribution repository
```

## Packages

- **`sql`**: Dialect-agnostic SQL storage adapter, schema migrations, connection pooling, and baseline data seeding.
- **`memory`**: In-memory, thread-safe persistence implementation for rapid unit and integration testing.

## Quality Gates

```bash
# Format check (gofmt)
test -z "$(gofmt -l .)"

# Unit & race detection tests (includes TestGoFmtCompliance)
go test -race -v ./...

# Static analysis & gofmt linter
golangci-lint run ./...
```

## License

AGPL-3.0
