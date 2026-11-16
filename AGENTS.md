# AGENTS.md

Idiomatic Go standards for `sql` (`github.com/resumeee-app/sql`). Run `make verify` after every change.

## Documentation

- **Package comments**: Place a single `Package <name> ...` doc comment in `<name>.go`.
- **Every symbol documented**: Write a Go doc comment starting with the exact identifier name for every exported *and* unexported type, function, method, constant, and package-level `var`.
- **Constants**: Document both grouped `const (...)` blocks and individual non-obvious constants with their unit, invariant, or purpose.
- **Small functions & why-comments**: Keep functions short, focused, and shallowly nested—extract well-named helpers rather than writing long multi-phase queries or migrations. Write inline comments to explain *why* (invariants, dialect quirks, transaction ordering), never to narrate syntax.

## Interfaces & Type Safety

- **Consumer-side interfaces**: Implement `core` store/repository contracts without leaking GORM or SQL types across package boundaries. Define any internal storage abstractions in the package that consumes them.
- **Accept interfaces, return structs**: Accept interfaces in constructors/functions when abstraction is needed, and return concrete struct pointers (`*Store`, `*Repo`).
- **Compile-time implementation checks**: Verify interface satisfaction at compile time with `var _ Interface = (*ConcreteType)(nil)` in the implementing package.
- **Typed contracts over assertions**: Pass concrete types or typed interfaces instead of `any` with runtime type assertions (`v.(T)`). Restrict `any` and type switches strictly to dynamic JSON column scanning/marshaling boundaries.

## Naming & Package Structure

- **Organize by domain, not technical layer**: Group code into cohesive domain packages (`sql`). Avoid generic or layered buckets (`utils`, `helpers`, `common`, `lib`, `tools`, `interfaces`, `models`).
- **File names**: Name files after the specific entity or concern they implement (`user_repo.go`, `branch_repo.go`, `dialect.go`, `open.go`). Avoid generic catch-all files (`interfaces.go`, `util.go`, `helpers.go`, `types.go`). When refactoring existing catch-all files, split them into focused domain files.
- **Package names & stuttering**: Use short, lowercase, singular nouns matching the directory name. Omit the package name from exported identifiers (`sql.Store`, not `sql.SQLStore`).
- **Identifiers**:
  - Apply Go initialisms consistently (`ID`, `URL`, `JSON`, `SQL`, `DB` — e.g. `UserID`, `openDB`).
  - Omit `Get` on simple getters (`DB()` rather than `GetDB()`).
  - Name constructors `New` or `New<Type>`, and sentinel errors `Err<Name>`.
  - Use a consistent 1–2 letter receiver name derived from the type across all methods (never `this` or `self`).
- **Dependency boundaries**: Encapsulate GORM models, callbacks, and SQLite/SQL dialect details inside package `sql`; map to/from `core` domain types at the repository boundary.

## Verification

- Run `make verify` (`gofmt`, `go test -race`, `golangci-lint`) before concluding any task and report only observed results.
