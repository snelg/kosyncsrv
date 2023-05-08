package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

type requestUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type requestHeader struct {
	Accept   string `header:"accept"`
	AuthUser string `header:"x-auth-user"`
	AuthKey  string `header:"x-auth-key"`
}

type requestPosition struct {
	Timestamp  int64       `json:"timestamp"`
	DocumentID string      `json:"document"`
	Progress   stringOrInt `json:"progress"`
	Device     string      `json:"device"`
	Percentage float64     `json:"percentage"`
	DeviceID   string      `json:"device_id"`
}

type replyPosition struct {
	Timestamp  int64  `json:"timestamp"`
	DocumentID string `json:"document"`
}

type requestDocid struct {
	DocumentID string `uri:"document" binding:"required"`
}

// Depending on wheather the document has pages, Koreader may send progress as a string or int.
// This is a helper type to facilicate marshaling and unmarshaling.
type stringOrInt struct {
	inner string
}

func (s stringOrInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

func (s *stringOrInt) UnmarshalJSON(b []byte) error {
	var i int
	err := json.Unmarshal(b, &i)
	if err == nil {
		*s = stringOrInt{strconv.Itoa(i)}
		return nil
	}

	var ss string
	err = json.Unmarshal(b, &ss)
	if err == nil {
		*s = stringOrInt{ss}
		return nil
	}

	return err
}

func validKeyField(field string) bool {
	return len(field) > 0 && !strings.Contains(field, ":")
}

func register(c *gin.Context) {
	var rUser requestUser
	if err := c.ShouldBindJSON(&rUser); err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 2003, "message": "Invalid request"})
		return
	}

	if rUser.Username == "" || rUser.Password == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 2003, "message": "Invalid request"})
		return
	}
	if !addDBUser(rUser.Username, rUser.Password) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 2002, "message": "Username is already registered."})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"username": rUser.Username,
	})
}

func authorize(c *gin.Context) {
	c.JSON(200, gin.H{
		"authorized": "OK",
	})
}

func getProgress(c *gin.Context) {
	username := c.MustGet("username").(string)
	var rDocid requestDocid
	if err := c.ShouldBindUri(&rDocid); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"msg": err})
		return
	}
	position, err := getDBPosition(username, rDocid.DocumentID)
	if err != nil {
		c.JSON(http.StatusOK, struct{}{})
	} else {
		c.JSON(http.StatusOK, position)
	}
}

func updateProgress(c *gin.Context) {
	username := c.MustGet("username").(string)
	var rPosition requestPosition
	var reply replyPosition

	if err := c.ShouldBindJSON(&rPosition); err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 2003, "message": "Invalid request, ddue"})
		return
	}
	if !validKeyField(rPosition.DocumentID) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 2004, "message": "Field 'document' not provided."})
		return
	}
	if rPosition.Progress.inner == "" || rPosition.Device == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 2003, "message": "Invalid request"})
		return
	}
	updatetime := updateDBdocument(username, rPosition)
	reply.DocumentID = rPosition.DocumentID
	reply.Timestamp = updatetime
	c.JSON(http.StatusOK, reply)
}

func AcceptHeaderCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		var header requestHeader
		if err := c.ShouldBindHeader(&header); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    100,
				"message": "Invalid Header",
			})
			c.Abort()
			return
		}
		if header.Accept == "application/vnd.koreader.v1+json" {
			c.Next()
			return
		}
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"code":    101,
			"message": "Invalid Accept header format.",
		})
		c.Abort()
	}
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		var header requestHeader
		unauthorizedError := gin.H{
			"code":    2001,
			"message": "Unauthorized",
		}
		_ = c.ShouldBindHeader(&header) // Ignore error handling, because we already did same binding in AcceptHeaderCheck
		if validKeyField(header.AuthUser) && len(header.AuthKey) > 0 {
			dUser, norows := getDBUser(header.AuthUser)
			if !norows && header.AuthKey == dUser.Password {
				c.Set("username", header.AuthUser)
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, unauthorizedError)
	}
}

func main() {
	dbfile := flag.String("d", "syncdata.db", "Sqlite3 DB file name")
	dbname = *dbfile
	srvhost := flag.String("t", "0.0.0.0", "Server host")
	srvport := flag.Int("p", 8080, "Server port")
	sslswitch := flag.Bool("ssl", false, "Start with https")
	sslc := flag.String("c", "", "SSL Certificate file")
	sslk := flag.String("k", "", "SSL Private key file")
	bindsrv := *srvhost + ":" + fmt.Sprint(*srvport)
	flag.Usage = func() {
		fmt.Println(`Usage: kosyncsrv [-h] [-t 127.0.0.1] [-p 8080] [-ssl -c "./cert.pem" -k "./cert.key"]`)
		flag.PrintDefaults()
	}
	flag.Parse()
	initDB()

	router := gin.Default()
	router.Use(AcceptHeaderCheck())
	router.GET("/healthcheck", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"state": "OK"})
	})
	router.POST("/users/create", register)
	authorized := router.Group("/", AuthRequired())
	{
		authorized.GET("/users/auth", authorize)
		authorized.GET("/syncs/progress/:document", getProgress)
		authorized.PUT("/syncs/progress", updateProgress)
	}
	if *sslswitch {
		router.RunTLS(bindsrv, *sslc, *sslk)
	} else {
		router.Run(bindsrv)
	}
}
