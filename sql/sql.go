// Package sql provides multi-database relational persistence implementations for Resumeee repositories,
// schema migrations, connection pooling, and baseline seed utilities.
//
// Implemented repository interfaces include:
//   - core.ProfileRepository: Main profiles, career graphs, and revision increments
//   - core.BranchRepository: Targeted resume branches and immutable version snapshots
//   - core.TemplateRepository: Built-in ATS and custom resume rendering templates
//   - core.PresetRepository: Built-in and user-defined tailoring presets
//   - core.PDFParseJobRepository: Asynchronous PDF resume parsing job status tracking
//   - core.QuotaRepository: Multi-tenant token and request rate limit accounting
//   - core.UserRepository: Tenant identity and role management
package sql
