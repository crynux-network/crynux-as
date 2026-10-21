package main

import (
	"flag"
	"fmt"
	"os"

	"crynux_as/migrate"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	rollback := flag.Bool("rollback", false, "rollback the last migration instead of migrating")
	flag.Parse()

	dsn := os.Getenv("AS_TEST_DB_DSN")
	if dsn == "" {
		dsn = "crynux_as:astestpass@tcp(127.0.0.1:3306)/crynux_as_test?parseTime=true&collation=utf8mb4_unicode_ci"
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to MySQL: %v\n", err)
		os.Exit(1)
	}

	migrate.InitMigration(db)

	if *rollback {
		if err := migrate.Rollback(); err != nil {
			fmt.Fprintf(os.Stderr, "rollback failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("rollback of the last migration succeeded")
		return
	}

	if err := migrate.Migrate(); err != nil {
		fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("all migrations applied successfully")
}
