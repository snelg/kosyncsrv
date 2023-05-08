package main

import (
	"database/sql"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var schemaUser string = `
CREATE TABLE IF NOT EXISTS "user" (
	"username"  TEXT(255),
	"password"  TEXT(255)
);
`
var schemaDocument string = `
CREATE TABLE IF NOT EXISTS "document" (
	"username"  TEXT(255),
	"documentid"  TEXT(255),
	"percentage"  REAL(64,4),
	"progress"  TEXT(255),
	"device"  TEXT(255),
	"device_id"  TEXT(255),
	"timestamp"  INTEGER
);`

var (
	db     *sqlx.DB
	dbname string
)

type dbUser struct {
	Username string `db:"username"`
	Password string `db:"password"`
}

type dbDocument struct {
	Username   string  `db:"username"`
	DocumentID string  `db:"documentid"`
	Percentage float64 `db:"percentage"`
	Progress   string  `db:"progress"`
	Device     string  `db:"device"`
	DeviceID   string  `db:"device_id"`
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

func getDBUser(username string) (dbUser, bool) {
	var result dbUser
	var norows bool = false
	err := db.Get(&result, "SELECT * FROM user WHERE username=$1", username)
	if err != nil {
		log.Println(err)
		if err == sql.ErrNoRows {
			norows = true
		}
	}
	return result, norows
}

func addDBUser(username string, password string) bool {
	_, norows := getDBUser(username)
	if norows {
		tx := db.MustBegin()
		tx.MustExec("INSERT INTO user (username, password) VALUES ($1, $2)", username, password)
		tx.Commit()
		return true
	}
	return false
}

func getDBPosition(username string, documentid string) (requestPosition, error) {
	var rPos requestPosition
	var resultDBdoc dbDocument
	err := db.Get(&resultDBdoc, "SELECT * FROM document WHERE document.username=$1 AND document.documentid=$2 ORDER BY document.timestamp DESC", username, documentid)
	if err != nil {
		log.Println(err)
		return rPos, err
	}
	rPos.Timestamp = resultDBdoc.Timestamp
	rPos.DocumentID = documentid
	rPos.Percentage = resultDBdoc.Percentage
	rPos.Progress = stringOrInt{resultDBdoc.Progress}
	rPos.Device = resultDBdoc.Device
	rPos.DeviceID = resultDBdoc.DeviceID
	return rPos, err
}

func existDoc(username string, docid string) bool {
	var result dbDocument
	err := db.Get(&result, "SELECT * FROM document WHERE username=$1 AND documentid=$2", username, docid)
	if err != nil {
		log.Println(err)
		if err == sql.ErrNoRows {
			return false
		}
	}
	return true
}

func updateDBdocument(username string, rPos requestPosition) int64 {
	nowtime := time.Now().Unix()
	if existDoc(username, rPos.DocumentID) {
		_, err := db.NamedExec("UPDATE document set percentage=:perc, progress=:prog, device=:device, device_id=:devid, timestamp=:time WHERE username=:user AND documentid=:docid", map[string]interface{}{
			"perc":   rPos.Percentage,
			"prog":   rPos.Progress.inner,
			"device": rPos.Device,
			"devid":  rPos.DeviceID,
			"time":   nowtime,
			"user":   username,
			"docid":  rPos.DocumentID,
		})
		if err != nil {
			log.Fatalln(err)
		}
		return nowtime
	}
	_, err := db.NamedExec("INSERT INTO document (username, documentid, percentage, progress, device, device_id, timestamp) VALUES (:user, :docid, :perc, :prog, :dev, :devid, :time)", map[string]interface{}{
		"user":  username,
		"docid": rPos.DocumentID,
		"perc":  rPos.Percentage,
		"prog":  rPos.Progress.inner,
		"dev":   rPos.Device,
		"devid": rPos.DeviceID,
		"time":  nowtime,
	})
	if err != nil {
		log.Fatalln(err)
	}
	return nowtime
}
