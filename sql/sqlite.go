// Package sqlite provides SQLite persistence implementations for Resumeee repositories,
// schema migrations, WAL pragma configuration, and baseline seed utilities.
//
// Implemented repository interfaces include:
//   - domain.ProfileRepository: Master profiles, career graphs, and revision increments
//   - domain.BranchRepository: Targeted resume branches and immutable version snapshots
//   - domain.TemplateRepository: Built-in ATS and custom resume rendering templates
//   - domain.PresetRepository: Built-in and user-defined tailoring presets
//   - domain.PDFParseJobRepository: Asynchronous PDF resume parsing job status tracking
package sqlite
