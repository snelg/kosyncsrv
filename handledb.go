package main

import (
	"database/sql"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var schemaUser = `
CREATE TABLE IF NOT EXISTS "user" (
	"username"  TEXT(255),
	"password"  TEXT(255)
);
CREATE UNIQUE INDEX IF NOT EXISTS username ON user(username);
`
var schemaDocument = `
CREATE TABLE IF NOT EXISTS "document" (
	"username"  TEXT(255),
	"documentid"  TEXT(255),
	"percentage"  REAL(64,4),
	"progress"  TEXT(255),
	"device"  TEXT(255),
	"device_id"  TEXT(255),
	"timestamp"  INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS username_documentid ON document(username,documentid);
`

var (
	db     *sqlx.DB
	dbname string
)

type DbUser struct {
	Username string `db:"username"`
	Password string `db:"password"`
}

type DbDocument struct {
	Username   string  `db:"username"`
	DocumentID string  `db:"documentid"`
	Percentage float64 `db:"percentage"`
	Progress   string  `db:"progress"`
	Device     string  `db:"device"`
	DeviceId   string  `db:"device_id"`
	Timestamp  int64   `db:"timestamp"`
}

func initDB() {
	var err error
	db, err = sqlx.Connect("sqlite3", dbname)
	if err != nil {
		log.Fatalln(err)
	}
	db.MustExec(schemaUser)
	db.MustExec(schemaDocument)
}

func getDBUser(username string) (DbUser, bool) {
	var user DbUser
	var noRows = false
	err := db.Get(&user, "SELECT * FROM user WHERE username=$1", username)
	if err != nil {
		log.Println(err)
		if err == sql.ErrNoRows {
			noRows = true
		}
	}
	return user, noRows
}

func addDBUser(username string, password string) bool {
	// Unique constraint will cause error if username already exists
	_, err := db.Exec("INSERT INTO user (username, password) VALUES ($1, $2)", username, password)
	return err == nil
}

func getDBDocument(username string, documentId string) (Document, error) {
	var document Document
	var dbDocument DbDocument
	err := db.Get(&dbDocument, "SELECT * FROM document WHERE document.username=$1 AND document.documentid=$2 ORDER BY document.timestamp DESC", username, documentId)
	if err != nil {
		log.Println(err)
		return document, err
	}
	document.Timestamp = dbDocument.Timestamp
	document.DocumentId = documentId
	document.Percentage = dbDocument.Percentage
	document.Progress = &StringOrInt{dbDocument.Progress}
	document.Device = dbDocument.Device
	document.DeviceId = dbDocument.DeviceId
	return document, nil
}

func updateDBDocument(username string, document Document) int64 {
	now := time.Now().Unix()
	_, err := db.NamedExec(
		`
			INSERT INTO document (username, documentid, percentage, progress, device, device_id, timestamp)
			VALUES (:user, :docid, :perc, :prog, :dev, :devid, :time)
			ON CONFLICT(username, documentid)
			DO UPDATE SET percentage=:perc, progress=:prog, device=:dev, device_id=:devid, timestamp=:time
		`,
		map[string]interface{}{
			"user":  username,
			"docid": document.DocumentId,
			"perc":  document.Percentage,
			"prog":  document.Progress.inner,
			"dev":   document.Device,
			"devid": document.DeviceId,
			"time":  now,
		})
	if err != nil {
		log.Fatalln(err)
	}
	return now
}
