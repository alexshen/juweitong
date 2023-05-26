package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/alexshen/juweitong/atom"
	"github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	myioutil "github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	"github.com/google/uuid"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/samber/lo"
)

var (
	fPort              = flag.Int("port", 8080, "listening port")
	fMaxAge            = flag.Int("age", 600, "max age of a session in second")
	fHttp              = flag.Bool("http", false, "run in http mode")
	fCABundle          = flag.String("ca", "", "path to the ca bundle file")
	fCert              = flag.String("cert", "", "path to the cert file")
	fPrivateKey        = flag.String("key", "", "path to the private key")
	fLog               = flag.String("log", "", "path to the log file, if empty, logging to os.Stdout")
	fOutRequestTimeout = flag.Int("timeout", 60, "seconds before an outgoing request times out")
)

var (
	clientMgr       *AtomClientManager
	errUnauthorized = errors.New("Unauthorized")
	logFile         *os.File
)

const (
	sessionIdName = "jwt_id"
)

type clientInstance struct {
	id string
	*atom.Client
	t *time.Timer
}

// touch restarts the timeout timer with timeout d
func (o *clientInstance) touch(d time.Duration, onTimeout func()) {
	o.stopTimer()
	o.t = time.AfterFunc(d, onTimeout)
}

// stopTimer stops the timeout timer
func (o *clientInstance) stopTimer() {
	if o.t != nil {
		o.t.Stop()
	}
}

type AtomClientManager struct {
	mtx     sync.Mutex
	clients map[string]*clientInstance
	maxAge  time.Duration
}

func NewAtomClientManager(maxAge time.Duration) *AtomClientManager {
	return &AtomClientManager{
		clients: make(map[string]*clientInstance),
		maxAge:  maxAge,
	}
}

// Get returns an existing atom.Client
func (mgr *AtomClientManager) Get(w http.ResponseWriter, req *http.Request) (*clientInstance, error) {
	cookie, err := req.Cookie(sessionIdName)
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if err == http.ErrNoCookie {
		w.WriteHeader(http.StatusUnauthorized)
		return nil, errUnauthorized
	}
	inst, ok := mgr.clients[cookie.Value]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return nil, errUnauthorized
	}
	return inst, nil
}

// GetOrNew always returns a new atom.Client. If there is already an old atom.Client,
// StopQRLogin is called, then it is removed.
func (mgr *AtomClientManager) GetOrNew(w http.ResponseWriter, req *http.Request) (*clientInstance, error) {
	var id string

	cookie, err := req.Cookie(sessionIdName)
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()
	if err == nil {
		// remove the old clients
		if inst, ok := mgr.clients[cookie.Value]; ok {
			inst.StopQRLogin()
			delete(mgr.clients, cookie.Value)
		}
		id = cookie.Value
	} else {
		newId, err := uuid.NewRandom()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return nil, err
		}
		id = newId.String()
		http.SetCookie(w, &http.Cookie{
			Name:  sessionIdName,
			Value: id,
		})
	}
	inst := &clientInstance{id: id, Client: atom.NewClient()}
	inst.Client.SetTimeout(time.Duration(*fOutRequestTimeout) * time.Second)
	inst.touch(mgr.maxAge, func() {
		mgr.remove(id)
		log.Printf("removed %s", id)
	})
	mgr.clients[id] = inst
	return inst, nil
}

// remove deletes the client with the given id
func (mgr *AtomClientManager) remove(id string) {
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()
	if inst, ok := mgr.clients[id]; ok {
		inst.StopQRLogin()
		inst.stopTimer()
		delete(mgr.clients, id)
	}
}

// Stop stops all qr login process
func (mgr *AtomClientManager) Stop() {
	for _, inst := range mgr.clients {
		inst.StopQRLogin()
	}
}

type responseMessage struct {
	Success bool   `json:"success"`
	Err     string `json:"err,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, obj any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(obj); err != nil {
		log.Print(err)
	}
}

func writeSuccess(w http.ResponseWriter, data any) {
	writeJSON(w, responseMessage{
		Success: true,
		Data:    data,
	})
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, responseMessage{
		Success: false,
		Err:     err.Error(),
	})
}

func startQRLogin(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		Url string `json:"url"`
	}

	client, err := clientMgr.GetOrNew(w, r)
	if err != nil {
		log.Print(err)
		return
	}
	log.Printf("start qr login for %s", client.id)
	qrcodeUrl, err := client.StartQRLogin(func() {
		log.Printf("%s logged in", client.id)
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, responseData{qrcodeUrl})
}

func isLoggedIn(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		LoggedIn bool `json:"loggedin"`
	}

	client, err := clientMgr.Get(w, r)
	if err != nil {
		log.Print(err)
		return
	}

	writeSuccess(w, responseData{client.IsLoggedIn()})
}

func getCommunities(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		Names   []string `json:"names"`
		Current int      `json:"current"`
	}

	client, err := clientMgr.Get(w, r)
	if err != nil {
		log.Print(err)
		return
	}

	writeSuccess(w, responseData{
		Names: lo.Map(client.Communites, func(e atom.Community, i int) string {
			return e.Name
		}),
		Current: client.CurrentCommunityIndex(),
	})
}

func setCurrentCommunity(w http.ResponseWriter, r *http.Request) {
	type requestData struct {
		Current int    `json:"current,omitempty"`
		Name    string `json:"name"`
	}

	client, err := clientMgr.Get(w, r)
	if err != nil {
		log.Print(err)
		return
	}

	query := requestData{Current: -1}
	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		log.Printf("invalid query: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	index := query.Current
	if query.Current == -1 {
		_, index, _ = lo.FindIndexOf(client.Communites, func(e atom.Community) bool { return e.Name == query.Name })
		if index == -1 {
			log.Printf("invalid community name: %s", query.Name)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else if query.Current < 0 || query.Current >= len(client.Communites) {
		log.Printf("invalid community index: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := client.SetCurrentCommunity(index); err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, nil)
}

func likePosts(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		Count int `json:"count"`
	}

	type requestData responseData

	client, err := clientMgr.Get(w, r)
	if err != nil {
		return
	}

	var query requestData
	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		log.Printf("invalid query: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if query.Count <= 0 {
		log.Printf("invalid count %d", query.Count)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var numPosts int
	kind, _ := mux.Vars(r)["kind"]
	switch kind {
	case "notices":
		numPosts = client.LikeNotices(query.Count)
	case "moments":
		numPosts = client.LikeMoments(query.Count)
	case "ccpposts":
		numPosts = client.LikeCCPPosts(query.Count)
	case "proposals":
		numPosts = client.LikeProposals(query.Count)
	default:
		log.Printf("unhandled like kind: %s", kind)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	writeSuccess(w, responseData{numPosts})
}

// getCertFile returns the file path containing the cert file and ca bundle
func getCertFile(caBundlePath, certPath string) (string, error) {
	f, err := os.CreateTemp("", "atom-server-cert")
	if err != nil {
		return "", err
	}
	defer f.Close() // ignore
	if err := myioutil.ConcatFiles(f, certPath, caBundlePath); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func reopenLogFile() error {
	if *fLog == "" {
		return nil
	}
	f, err := os.OpenFile(*fLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if logFile != nil {
		logFile.Close()
	}
	logFile = f
	log.SetOutput(logFile)

	return nil
}

func main() {
	flag.Parse()
	clientMgr = NewAtomClientManager(time.Second * time.Duration(*fMaxAge))
	router := mux.NewRouter()
	router.HandleFunc("api/startqrlogin", startQRLogin).Methods("POST")
	router.HandleFunc("api/isloggedin", isLoggedIn).Methods("GET")
	router.HandleFunc("api/getcommunities", getCommunities).Methods("GET")
	router.HandleFunc("api/setcurrentcommunity", setCurrentCommunity).Methods("POST")
	router.HandleFunc("api/like{kind:notices|moments|ccpposts|proposals}", likePosts).Methods("POST")

	if err := reopenLogFile(); err != nil {
		log.Fatal(err)
	}
	logWriter := ioutil.NewRedirectableWriter(logFile)

	server := http.Server{
		Addr:         ":" + strconv.Itoa(*fPort),
		Handler:      handlers.LoggingHandler(logWriter, router),
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
	}
	log.Printf("starting server, listening at %s", server.Addr)

	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

		for {
			switch <-sigint {
			case os.Interrupt, syscall.SIGTERM:
				if err := server.Shutdown(context.Background()); err != nil {
					log.Printf("Shutdown: %v", err)
				}
				return
			case syscall.SIGHUP:
				// reopen the log file
				if err := reopenLogFile(); err != nil {
					log.Printf("unable to open log file: %v", err)
					break
				}
				log.Printf("log file reopened")
				logWriter.SetWriter(logFile)
			}
		}
	}()

	if *fHttp {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	} else {
		certFile, err := getCertFile(*fCABundle, *fCert)
		if err != nil {
			log.Fatalf("failed to create the cert file: %v", err)
		}
		defer os.Remove(certFile)
		if err := server.ListenAndServeTLS(certFile, *fPrivateKey); err != http.ErrServerClosed {
			log.Fatalf("ListenAndServeTLS: %v", err)
		}
	}

	<-shutdown
	clientMgr.Stop()

	log.Print("server has been shutdown")
	if logFile != nil {
		logFile.Close()
	}
}
