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

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Header struct {
	Accept   string `header:"accept"`
	AuthUser string `header:"x-auth-user"`
	AuthKey  string `header:"x-auth-key"`
}

type Document struct {
	DocumentId string       `json:"document" uri:"document" binding:"required"`
	Progress   *StringOrInt `json:"progress"`
	Device     string       `json:"device"`
	Percentage float64      `json:"percentage"`
	DeviceId   string       `json:"device_id"`
	Timestamp  int64        `json:"timestamp"`
}

type ErrorResponse struct {
	Status  int
	Code    int
	Message string
}

func (err *ErrorResponse) Error() string {
	return err.Message
}

var (
	InvalidHeader             = ErrorResponse{http.StatusBadRequest, 100, "Invalid Header"}
	InvalidAcceptHeader       = ErrorResponse{http.StatusPreconditionFailed, 101, "Invalid Accept header format."}
	UnknownServerError        = ErrorResponse{http.StatusInternalServerError, 500, "Unknown server error."}
	Unauthorized              = ErrorResponse{http.StatusUnauthorized, 2001, "Unauthorized"}
	UsernameAlreadyRegistered = ErrorResponse{http.StatusForbidden, 2002, "Username is already registered."}
	InvalidRequest            = ErrorResponse{http.StatusForbidden, 2003, "Invalid Request"}
	DocumentIdNotProvided     = ErrorResponse{http.StatusForbidden, 2004, "Field 'document' not provided."}
)

// StringOrInt Depending on whether the document has pages, KOReader may send progress as a string or int.
// This is a helper type to facilitate marshalling and unmarshalling.
type StringOrInt struct {
	inner string
}

func (s *StringOrInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

func (s *StringOrInt) UnmarshalJSON(b []byte) error {
	var i int
	err := json.Unmarshal(b, &i)
	if err == nil {
		*s = StringOrInt{strconv.Itoa(i)}
		return nil
	}

	var ss string
	err = json.Unmarshal(b, &ss)
	if err == nil {
		*s = StringOrInt{ss}
		return nil
	}

	return err
}

func validKeyField(field string) bool {
	return len(field) > 0 && !strings.Contains(field, ":")
}

func register(c *gin.Context) {
	var user User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.Error(&InvalidRequest)
		return
	}

	if user.Username == "" || user.Password == "" {
		c.Error(&InvalidRequest)
		return
	}
	if !addDBUser(user.Username, user.Password) {
		c.Error(&UsernameAlreadyRegistered)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"username": user.Username,
	})
}

func authorize(c *gin.Context) {
	c.JSON(200, gin.H{
		"authorized": "OK",
	})
}

func getProgress(c *gin.Context) {
	username := c.MustGet("header").(Header).AuthUser
	var requestDocument Document
	if err := c.ShouldBindUri(&requestDocument); err != nil {
		c.Error(&UnknownServerError)
		return
	}
	document, err := getDBDocument(username, requestDocument.DocumentId)
	if err != nil {
		c.JSON(http.StatusOK, struct{}{})
	} else {
		c.JSON(http.StatusOK, document)
	}
}

func updateProgress(c *gin.Context) {
	username := c.MustGet("header").(Header).AuthUser
	var requestDocument Document

	if err := c.ShouldBindJSON(&requestDocument); err != nil {
		// Semi-hacky; really should explicitly check if err is ValidationErrors and dig down in that
		errorMessage := err.Error()
		if strings.Contains(errorMessage, "DocumentId") && strings.Contains(errorMessage, "required") {
			c.Error(&DocumentIdNotProvided)
			return
		}
		c.Error(&InvalidRequest)
		return
	}
	if !validKeyField(requestDocument.DocumentId) {
		c.Error(&DocumentIdNotProvided)
		return
	}
	if requestDocument.Progress == nil || requestDocument.Device == "" {
		c.Error(&InvalidRequest)
		return
	}
	timestamp := updateDBDocument(username, requestDocument)
	c.JSON(http.StatusOK, gin.H{
		"timestamp": timestamp,
		"document":  requestDocument.DocumentId,
	})
}

func ErrorHandler(c *gin.Context) {
	c.Next()
	var err *ErrorResponse
	// This specific project only returns one error per call, so we don't need to loop through all c.Errors
	if len(c.Errors) > 0 && errors.As(c.Errors[0].Err, &err) {
		c.AbortWithStatusJSON(err.Status, gin.H{"code": err.Code, "message": err.Message})
	}
}

func AcceptHeaderCheck(c *gin.Context) {
	var header Header
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
	header := c.MustGet("header").(Header)
	if validKeyField(header.AuthUser) && len(header.AuthKey) > 0 {
		user, noRows := getDBUser(header.AuthUser)
		if !noRows && header.AuthKey == user.Password {
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
