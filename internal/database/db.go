package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func NewDB() *sql.DB {
	var (
		err error
		db  *sql.DB
	)

	db, err = sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}

	pingErr := db.Ping()
	if pingErr != nil {
		log.Fatal(pingErr)
	}
	fmt.Println("Connected")

	_, err = db.Exec(`
		CREATE TABLE messages(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			author VARCHAR(255),
			content TEXT,
			message_type INTEGER
		)`)

	if err != nil {
		log.Fatal(err)
	}

	return db
}
