package main

import (
	"flag"
	"fmt"
	"os"

	"crynux_as/migrate"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Runs the full migration chain from migrate.InitMigration against a local
// MySQL test database. Pass -rollback to roll back the last migration instead.
func main() {
	rollback := flag.Bool("rollback", false, "rollback the last migration instead of migrating")
	flag.Parse()

	dsn := os.Getenv("AS_TEST_DB_DSN")
	if dsn == "" {
		dsn = "crynux_as_migtest:astestpass@tcp(127.0.0.1:3306)/crynux_as_test?parseTime=true&collation=utf8mb4_unicode_ci"
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Println("failed to connect to database:", err)
		os.Exit(1)
	}

	migrate.InitMigration(db)

	if *rollback {
		if err := migrate.Rollback(); err != nil {
			fmt.Println("rollback failed:", err)
			os.Exit(1)
		}
		fmt.Println("last migration rolled back successfully")
		return
	}

	if err := migrate.Migrate(); err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Println("all migrations applied successfully")
}
