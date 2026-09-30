package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	dbFilename := flag.String("d", "syncdata.db", "Sqlite3 DB file name")
	host := flag.String("t", "0.0.0.0", "Server host")
	port := flag.Int("p", 8080, "Server port")
	useSSL := flag.Bool("ssl", false, "Start with https")
	sslCertFile := flag.String("c", "", "SSL Certificate file")
	sslKeyFile := flag.String("k", "", "SSL Private key file")
	flag.Usage = func() {
		fmt.Println(`Usage: kosyncsrv [-h] [-t 127.0.0.1] [-p 8080] [-ssl -c "./cert.pem" -k "./cert.key"]`)
		flag.PrintDefaults()
	}
	flag.Parse()

	dbname = *dbFilename
	serverAddr := *host + ":" + fmt.Sprint(*port)

	initDB()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthcheck", healthcheck)
	mux.HandleFunc("POST /users/create", register)
	mux.HandleFunc("GET /users/auth", authorize)
	mux.HandleFunc("PUT /syncs/progress", updateProgress)
	mux.HandleFunc("GET /syncs/progress/{document}", getProgress)

	wrappedMux := acceptHeaderCheck(mux)

	fmt.Println("starting server:", serverAddr)
	var err error
	if *useSSL {
		err = http.ListenAndServeTLS(serverAddr, *sslCertFile, *sslKeyFile, wrappedMux)
	} else {
		err = http.ListenAndServe(serverAddr, wrappedMux)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Println("server stop:", err)
	}
}

func acceptHeaderCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("accept") != "application/vnd.koreader.v1+json" {
			handleError(w, r, InvalidAcceptHeader)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleError(w http.ResponseWriter, r *http.Request, err ErrorResponse) {
	w.WriteHeader(err.Status)
	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(marshal(jObj{"code": err.Code, "message": err.Message}))
}

func healthcheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(marshal(jObj{"state": "OK"}))
}

func register(w http.ResponseWriter, r *http.Request) {
	var user User
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		handleError(w, r, UnknownServerError)
		return
	}

	_ = json.Unmarshal(rawBody, &user) // don't need to capture error, because in case of error the Username and Password fields will be blank, which we catch next
	if user.Username == "" || user.Password == "" {
		handleError(w, r, InvalidRequest)
		return
	}

	if !validKeyField(user.Username) {
		handleError(w, r, InvalidRequest)
		return
	}

	if !addDBUser(user.Username, user.Password) {
		handleError(w, r, UsernameAlreadyRegistered)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(marshal(jObj{"username": user.Username}))
}

func authorize(w http.ResponseWriter, r *http.Request) {
	user := authorizedUser(r)
	if user == "" {
		handleError(w, r, Unauthorized)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(marshal(jObj{"authorized": "OK"}))
}
func updateProgress(w http.ResponseWriter, r *http.Request) {
	username := authorizedUser(r)
	if username == "" {
		handleError(w, r, Unauthorized)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		handleError(w, r, UnknownServerError)
		return
	}

	var requestDocument Document
	err = json.Unmarshal(rawBody, &requestDocument)
	if err != nil {
		handleError(w, r, InvalidRequest)
		return
	}
	requestDocument.DocumentId = strings.TrimSpace(requestDocument.DocumentId)
	requestDocument.Device = strings.TrimSpace(requestDocument.Device)

	if !validKeyField(requestDocument.DocumentId) {
		handleError(w, r, DocumentIdNotProvided)
		return
	}
	if requestDocument.Progress == nil || requestDocument.Device == "" {
		handleError(w, r, InvalidRequest)
		return
	}

	timestamp := updateDBDocument(username, requestDocument)

	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(marshal(jObj{
		"timestamp": timestamp,
		"document":  requestDocument.DocumentId,
	}))
}

func getProgress(w http.ResponseWriter, r *http.Request) {
	username := authorizedUser(r)
	if username == "" {
		handleError(w, r, Unauthorized)
		return
	}

	document, err := getDBDocument(username, r.PathValue("document"))
	w.Header().Add("Content-Type", "application/json")
	if err != nil {
		_, _ = w.Write(marshal(jObj{}))
	} else {
		_, _ = w.Write(marshal(document))
	}
}

func validKeyField(field string) bool {
	return len(field) > 0 && !strings.Contains(field, ":")
}

func marshal(data any) []byte {
	out, _ := json.Marshal(data)
	return out
}

func authorizedUser(r *http.Request) string {
	username := strings.TrimSpace(r.Header.Get("x-auth-user"))
	key := strings.TrimSpace(r.Header.Get("x-auth-key"))
	if key == "" || !validKeyField(username) {
		return ""
	}
	user, noRows := getDBUser(username)
	if noRows || key != user.Password {
		return ""
	}

	return username
}

type jObj map[string]any

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Document struct {
	DocumentId string       `json:"document"`
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

func (err ErrorResponse) Error() string {
	return err.Message
}

var (
	InvalidAcceptHeader       = ErrorResponse{http.StatusPreconditionFailed, 101, "Invalid Accept header format."}
	UnknownServerError        = ErrorResponse{http.StatusInternalServerError, 500, "Unknown server error."}
	Unauthorized              = ErrorResponse{http.StatusUnauthorized, 2001, "Unauthorized"}
	UsernameAlreadyRegistered = ErrorResponse{http.StatusPaymentRequired, 2002, "Username is already registered."}
	InvalidRequest            = ErrorResponse{http.StatusForbidden, 2003, "Invalid Request"}
	DocumentIdNotProvided     = ErrorResponse{http.StatusForbidden, 2004, "Field 'document' not provided."}
)

// StringOrInt Depending on whether the document has pages, KOReader may send progress as a string or int.
// This is a helper type to facilitate marshaling and unmarshaling.
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
