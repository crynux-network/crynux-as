package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func M20261101(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261101",
		Migrate: func(tx *gorm.DB) error {
			// Round half-up to the nearest 1000 Gwei (1 kgwei) and enforce a
			// minimum of 1000 Gwei. Existing project counts are small, so one
			// statement is enough. Rollback does not restore pre-round values.
			return tx.Exec(`
UPDATE projects
SET priority_gwei = CAST(
	GREATEST(
		1000,
		FLOOR((CAST(priority_gwei AS DECIMAL(65, 0)) + 500) / 1000) * 1000
	) AS CHAR
)
WHERE CAST(priority_gwei AS DECIMAL(65, 0)) < 1000
   OR MOD(CAST(priority_gwei AS DECIMAL(65, 0)), 1000) <> 0
`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return nil
		},
	}})
}
