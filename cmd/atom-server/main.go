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
	"time"

	"github.com/alexshen/juweitong/atom"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/samber/lo"
)

var (
	fPort   = flag.Int("port", 8080, "listening port")
	fMaxAge = flag.Int("age", 600, "max age of a session in second")
)

var (
	clientMgr       *AtomClientManager
	errUnauthorized = errors.New("Unauthorized")
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

func likePosts(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		Count int `json:"count"`
	}

	client, err := clientMgr.Get(w, r)
	if err != nil {
		return
	}

	value := r.URL.Query().Get("count")
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	kind, _ := mux.Vars(r)["kind"]
	switch kind {
	case "notices":
		client.LikeNotices(count)
	case "moments":
		client.LikeMoments(count)
	case "ccpposts":
		client.LikeCCPPosts(count)
	case "proposals":
		client.LikeProposals(count)
	default:
		log.Printf("unhandled like kind: %s", kind)
		w.WriteHeader(http.StatusBadRequest)
	}
}

func main() {
	flag.Parse()
	clientMgr = NewAtomClientManager(time.Second * time.Duration(*fMaxAge))
	router := mux.NewRouter()
	router.HandleFunc("/startqrlogin", startQRLogin).Methods("POST")
	router.HandleFunc("/isloggedin", isLoggedIn).Methods("GET")
	router.HandleFunc("/getcommunities", getCommunities).Methods("GET")
	router.HandleFunc("/like{kind:notices|moments|ccpposts|proposals}", likePosts).Methods("POST")

	server := http.Server{
		Addr:         ":" + strconv.Itoa(*fPort),
		Handler:      router,
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
	}
	log.Printf("starting server, listening at %s", server.Addr)

	shutdown := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)

		<-sigint

		if err := server.Shutdown(context.Background()); err != nil {
			log.Printf("Shutdown: %v", err)
		}

		close(shutdown)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("ListenAndServe: %v", err)
	}

	<-shutdown
	clientMgr.Stop()

	log.Print("server has been shutdown")
}
