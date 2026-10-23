package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"resumeee/internal/domain"
)

const (
	// Built-in Template Set IDs
	TemplateStandardCleanID        = "builtin-standard-clean"
	TemplateDetailedImmigrationCVID = "builtin-detailed-immigration-cv"

	// Built-in Preset IDs
	Preset1PageTechResumeID          = "builtin-1-page-tech-resume"
	PresetImmigrationFullHistoryCVID = "builtin-immigration-full-history-cv"
)

// SeedDefaultData idempotently populates default user profile, built-in template sets,
// and built-in presets into SQLite on first boot.
func SeedDefaultData(ctx context.Context, db *gorm.DB, defaultUserID ...string) error {
	targetUserID := "default"
	if len(defaultUserID) > 0 && defaultUserID[0] != "" {
		targetUserID = defaultUserID[0]
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Seed Built-in Template Sets
		templates := getBuiltinTemplateSets()
		for _, tmpl := range templates {
			var existing domain.TemplateSet
			err := tx.Where("id = ?", tmpl.ID).First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := ValidateTemplateSet(&tmpl); err != nil {
						return fmt.Errorf("validating built-in template %s: %w", tmpl.Name, err)
					}
					if err := tx.Create(&tmpl).Error; err != nil {
						return fmt.Errorf("seeding template %s: %w", tmpl.Name, err)
					}
				} else {
					return err
				}
			}
		}

		// 2. Seed Built-in Presets
		presets := getBuiltinPresets(targetUserID)
		for _, preset := range presets {
			var existing domain.Preset
			err := tx.Where("id = ?", preset.ID).First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := tx.Create(&preset).Error; err != nil {
						return fmt.Errorf("seeding preset %s: %w", preset.Name, err)
					}
				} else {
					return err
				}
			}
		}

		// 3. Seed Default Profile
		var existingProf domain.Profile
		err := tx.Where("user_id = ?", targetUserID).First(&existingProf).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				defaultProfile := domain.Profile{
					ID:                 "profile-" + targetUserID,
					UserID:             targetUserID,
					FullName:           "Default User",
					Headline:           "Software Engineer",
					MasterRevision:     1,
					DefaultProvider:    "gemini",
					DefaultModel:       "gemini-2.5-pro",
					GlobalWritingRules: "Prioritize measurable outcomes, active voice, and clear technical context.",
					CreatedAt:          time.Now().UTC(),
					UpdatedAt:          time.Now().UTC(),
				}
				if err := tx.Create(&defaultProfile).Error; err != nil {
					return fmt.Errorf("seeding default profile: %w", err)
				}
			} else {
				return err
			}
		}

		return nil
	})
}

func getBuiltinTemplateSets() []domain.TemplateSet {
	now := time.Now().UTC()
	return []domain.TemplateSet{
		{
			ID:          TemplateStandardCleanID,
			UserID:      "system",
			Name:        "Standard Clean",
			Description: "Clean modern single or multi-page resume with embedded CSS styles.",
			IsBuiltIn:   true,
			CSSStyles: `body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.5; color: #1a1a1a; margin: 0; padding: 24px; }
.resume { max-width: 800px; margin: 0 auto; }
.header { border-bottom: 2px solid #2563eb; padding-bottom: 12px; margin-bottom: 20px; }
h1 { margin: 0 0 4px 0; font-size: 28px; }
.headline { font-size: 16px; color: #4b5563; }
.contact { font-size: 14px; color: #6b7280; margin-top: 4px; }
h2 { font-size: 18px; border-bottom: 1px solid #e5e7eb; padding-bottom: 4px; margin-top: 20px; color: #1e3a8a; }
h3 { font-size: 15px; margin: 8px 0 2px 0; }
.meta { font-size: 13px; color: #6b7280; }
ul { margin: 6px 0 12px 18px; padding: 0; }
li { margin-bottom: 4px; }`,
			HTMLTemplate: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>{{.FullName}} - Resume</title>
<style>{{.CSSStyles}}</style>
</head>
<body>
<div class="resume">
  <header class="header">
    <h1>{{.FullName}}</h1>
    <div class="headline">{{.Headline}}</div>
    <div class="contact">{{.Email}} | {{.Phone}}</div>
  </header>
  <section class="summary">
    <h2>Summary</h2>
    <p>{{.Summary}}</p>
  </section>
  <section class="experience">
    <h2>Experience</h2>
    {{range .Experiences}}
    <div class="job">
      <h3>{{.RoleTitle}} - {{.Company}}</h3>
      <div class="meta">{{.StartDate}} - {{if .EndDate}}{{.EndDate}}{{else}}Present{{end}}</div>
      <ul>
        {{range .BulletPoints}}
        <li>{{.Text}}</li>
        {{end}}
      </ul>
    </div>
    {{end}}
  </section>
</div>
</body>
</html>`,
			MarkdownTemplate: `# {{.FullName}}
**{{.Headline}}** | {{.Email}} | {{.Phone}}

## Professional Summary
{{.Summary}}

## Work Experience
{{range .Experiences}}
### {{.RoleTitle}} — {{.Company}}
*{{.StartDate}} – {{if .EndDate}}{{.EndDate}}{{else}}Present{{end}}*

{{range .BulletPoints}}
- {{.Text}}
{{end}}
{{end}}`,
			TextTemplate: `{{.FullName}}
{{.Headline}} | {{.Email}} | {{.Phone}}

SUMMARY:
{{.Summary}}

EXPERIENCE:
{{range .Experiences}}
{{.RoleTitle}} at {{.Company}} ({{.StartDate}} - {{if .EndDate}}{{.EndDate}}{{else}}Present{{end}})
{{range .BulletPoints}}
* {{.Text}}
{{end}}
{{end}}`,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          TemplateDetailedImmigrationCVID,
			UserID:      "system",
			Name:        "Detailed Immigration / CV",
			Description: "Complete career history template with hours per week, supervisor contact, and full facts.",
			IsBuiltIn:   true,
			CSSStyles: `body { font-family: "Times New Roman", Times, serif; line-height: 1.6; color: #111; margin: 0; padding: 32px; }
.cv { max-width: 850px; margin: 0 auto; }
.header { border-bottom: 2px solid #000; padding-bottom: 12px; margin-bottom: 24px; text-align: center; }
h1 { margin: 0 0 6px 0; font-size: 26px; text-transform: uppercase; letter-spacing: 1px; }
.headline { font-size: 15px; font-style: italic; }
.contact { font-size: 13px; margin-top: 6px; }
h2 { font-size: 17px; border-bottom: 1px solid #444; padding-bottom: 3px; margin-top: 24px; text-transform: uppercase; }
h3 { font-size: 15px; margin: 12px 0 4px 0; }
.meta { font-size: 13px; color: #333; margin-bottom: 6px; }
.duties { font-style: italic; margin: 4px 0; }
ul { margin: 6px 0 16px 20px; padding: 0; }
li { margin-bottom: 4px; }`,
			HTMLTemplate: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>{{.FullName}} - Comprehensive Curriculum Vitae</title>
<style>{{.CSSStyles}}</style>
</head>
<body>
<div class="cv">
  <header class="header">
    <h1>{{.FullName}}</h1>
    <div class="headline">{{.Headline}}</div>
    <div class="contact">Email: {{.Email}} | Phone: {{.Phone}}</div>
  </header>
  <section class="summary">
    <h2>Professional Statement</h2>
    <p>{{.Summary}}</p>
  </section>
  <section class="experience">
    <h2>Comprehensive Employment & Verification History</h2>
    {{range .Experiences}}
    <div class="record">
      <h3>{{.RoleTitle}} — {{.Company}}</h3>
      <div class="meta">
        <div><strong>Dates:</strong> {{.StartDate}} to {{if .EndDate}}{{.EndDate}}{{else}}Present{{end}}</div>
        {{if .HoursPerWeek}}<div><strong>Hours per Week:</strong> {{.HoursPerWeek}}</div>{{end}}
        {{if .SupervisorName}}<div><strong>Supervisor:</strong> {{.SupervisorName}} ({{.SupervisorContact}})</div>{{end}}
        {{if .DutiesSummary}}<p class="duties"><strong>Official Duties:</strong> {{.DutiesSummary}}</p>{{end}}
      </div>
      <ul>
        {{range .BulletPoints}}
        <li>{{.Text}}</li>
        {{end}}
      </ul>
    </div>
    {{end}}
  </section>
</div>
</body>
</html>`,
			MarkdownTemplate: `# {{.FullName}} — Comprehensive Curriculum Vitae
Email: {{.Email}} | Phone: {{.Phone}}

## Professional Statement
{{.Summary}}

## Comprehensive Employment History
{{range .Experiences}}
### {{.RoleTitle}} — {{.Company}}
- **Dates:** {{.StartDate}} to {{if .EndDate}}{{.EndDate}}{{else}}Present{{end}}
{{if .HoursPerWeek}}- **Hours per Week:** {{.HoursPerWeek}}{{end}}
{{if .SupervisorName}}- **Supervisor:** {{.SupervisorName}} ({{.SupervisorContact}}){{end}}
{{if .DutiesSummary}}- **Official Duties:** {{.DutiesSummary}}{{end}}

**Responsibilities & Verifiable Achievements:**
{{range .BulletPoints}}
- {{.Text}}
{{end}}
{{end}}`,
			TextTemplate: `{{.FullName}} - COMPREHENSIVE CURRICULUM VITAE
Email: {{.Email}} | Phone: {{.Phone}}

PROFESSIONAL STATEMENT:
{{.Summary}}

EMPLOYMENT HISTORY:
{{range .Experiences}}
{{.RoleTitle}} at {{.Company}}
Dates: {{.StartDate}} - {{if .EndDate}}{{.EndDate}}{{else}}Present{{end}}
{{if .HoursPerWeek}}Hours/Week: {{.HoursPerWeek}}{{end}}
{{if .SupervisorName}}Supervisor: {{.SupervisorName}} ({{.SupervisorContact}}){{end}}
{{range .BulletPoints}}
* {{.Text}}
{{end}}
{{end}}`,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func getBuiltinPresets(userID string) []domain.Preset {
	now := time.Now().UTC()
	return []domain.Preset{
		{
			ID:                  Preset1PageTechResumeID,
			UserID:              userID,
			Name:                "1-Page Tech Resume",
			Description:         "Concise tech resume format emphasizing recent high-impact metrics and key skills.",
			SystemInstructions:  "Focus strictly on high-impact engineering accomplishments, quantitative outcomes, and relevant technical skill groups. Ensure concise bullet formatting suitable for a 1-page tech resume.",
			TemplateSetID:       TemplateStandardCleanID,
			IncludeAdminFields:  false,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		{
			ID:                  PresetImmigrationFullHistoryCVID,
			UserID:              userID,
			Name:                "Immigration Full History CV",
			Description:         "Unabridged career history format for audit, visa, and immigration compliance.",
			SystemInstructions:  "Include full, unabridged employment and academic history. Maintain detailed hours per week, company location and verification contacts, supervisor contact information, and full explanatory duty summaries.",
			TemplateSetID:       TemplateDetailedImmigrationCVID,
			IncludeAdminFields:  true,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}
}
