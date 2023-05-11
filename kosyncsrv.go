package main

import (
	"encoding/json"
	"errors"
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

type errorResponse struct {
	Status  int
	Code    int
	Message string
}

func (err *errorResponse) Error() string {
	return err.Message
}

var (
	InvalidHeader             = errorResponse{http.StatusBadRequest, 100, "Invalid Header"}
	InvalidAcceptHeader       = errorResponse{http.StatusPreconditionFailed, 101, "Invalid Accept header format."}
	UnknownServerError        = errorResponse{http.StatusInternalServerError, 500, "Unknown server error."}
	Unauthorized              = errorResponse{http.StatusUnauthorized, 2001, "Unauthorized"}
	UsernameAlreadyRegistered = errorResponse{http.StatusForbidden, 2002, "Username is already registered."}
	InvalidRequest            = errorResponse{http.StatusForbidden, 2003, "Invalid Request"}
	DocumentIdNotProvided     = errorResponse{http.StatusForbidden, 2004, "Field 'document' not provided."}
)

// Depending on whether the document has pages, Koreader may send progress as a string or int.
// This is a helper type to facilitate marshaling and unmarshaling.
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
		c.Error(&InvalidRequest)
		return
	}

	if rUser.Username == "" || rUser.Password == "" {
		c.Error(&InvalidRequest)
		return
	}
	if !addDBUser(rUser.Username, rUser.Password) {
		c.Error(&UsernameAlreadyRegistered)
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
	username := c.MustGet("header").(requestHeader).AuthUser
	var rDocid requestDocid
	if err := c.ShouldBindUri(&rDocid); err != nil {
		c.Error(&UnknownServerError)
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
	username := c.MustGet("header").(requestHeader).AuthUser
	var rPosition requestPosition
	var reply replyPosition

	if err := c.ShouldBindJSON(&rPosition); err != nil {
		c.Error(&InvalidRequest)
		return
	}
	if !validKeyField(rPosition.DocumentID) {
		c.Error(&DocumentIdNotProvided)
		return
	}
	if rPosition.Progress.inner == "" || rPosition.Device == "" {
		c.Error(&InvalidRequest)
		return
	}
	updatetime := updateDBdocument(username, rPosition)
	reply.DocumentID = rPosition.DocumentID
	reply.Timestamp = updatetime
	c.JSON(http.StatusOK, reply)
}

func ErrorHandler(c *gin.Context) {
	c.Next()
	var err *errorResponse
	// This specific project only returns one error per call, so we don't need to loop through all c.Errors
	if len(c.Errors) > 0 && errors.As(c.Errors[0].Err, &err) {
		c.AbortWithStatusJSON(err.Status, gin.H{"code": err.Code, "message": err.Message})
	}
}

func AcceptHeaderCheck(c *gin.Context) {
	var header requestHeader
	if err := c.ShouldBindHeader(&header); err != nil {
		c.Error(&InvalidHeader)
		c.Abort()
		return
	}
	if header.Accept == "application/vnd.koreader.v1+json" {
		c.Set("header", header)
		c.Next()
		return
	}
	c.Error(&InvalidAcceptHeader)
	c.Abort()
}

func AuthRequired(c *gin.Context) {
	header := c.MustGet("header").(requestHeader)
	if validKeyField(header.AuthUser) && len(header.AuthKey) > 0 {
		dUser, norows := getDBUser(header.AuthUser)
		if !norows && header.AuthKey == dUser.Password {
			c.Set("username", header.AuthUser)
			c.Next()
			return
		}
	}

	c.Error(&Unauthorized)
	c.Abort()
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
	router.Use(ErrorHandler)
	router.Use(AcceptHeaderCheck)
	router.GET("/healthcheck", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"state": "OK"})
	})
	router.POST("/users/create", register)
	authorized := router.Group("/", AuthRequired)
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
