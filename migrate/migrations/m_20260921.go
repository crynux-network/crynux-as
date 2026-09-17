package migrations

import (
	"fmt"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const mysqlUnicodeCollation = "utf8mb4_unicode_ci"
const mysqlDefaultCollation = "utf8mb4_0900_ai_ci"

func convertMySQLDatabaseAndTables(tx *gorm.DB, collation string) error {
	if tx.Dialector.Name() != "mysql" {
		return nil
	}

	var databaseName string
	if err := tx.Raw("SELECT DATABASE()").Scan(&databaseName).Error; err != nil {
		return err
	}
	if databaseName == "" {
		return fmt.Errorf("current database is empty")
	}

	if err := tx.Exec(
		fmt.Sprintf(
			"ALTER DATABASE `%s` CHARACTER SET utf8mb4 COLLATE %s",
			databaseName,
			collation,
		),
	).Error; err != nil {
		return err
	}

	var tableNames []string
	if err := tx.Raw(
		`SELECT TABLE_NAME
		 FROM information_schema.TABLES
		 WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		 ORDER BY TABLE_NAME`,
		databaseName,
	).Scan(&tableNames).Error; err != nil {
		return err
	}

	for _, tableName := range tableNames {
		if err := tx.Exec(
			fmt.Sprintf(
				"ALTER TABLE `%s` CONVERT TO CHARACTER SET utf8mb4 COLLATE %s",
				tableName,
				collation,
			),
		).Error; err != nil {
			return err
		}
	}

	return nil
}

func M20260921(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260921",
			Migrate: func(tx *gorm.DB) error {
				return convertMySQLDatabaseAndTables(tx, mysqlUnicodeCollation)
			},
			Rollback: func(tx *gorm.DB) error {
				return convertMySQLDatabaseAndTables(tx, mysqlDefaultCollation)
			},
		},
	})
}
