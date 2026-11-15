package sql

import (
	"context"

	"github.com/resumeee-app/resumeee-core/seed"
	"github.com/resumeee-app/resumeee-core/store"
)

// SeedDefaultData idempotently populates built-in templates, presets, global skills,
// and the default user profile and branch into st.
func SeedDefaultData(ctx context.Context, st *store.Store, defaultUserID ...string) error {
	return seed.DefaultData(ctx, st, defaultUserID...)
}
