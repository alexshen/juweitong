package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/alexshen/juweitong/atom"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/samber/lo"
)

var (
	clientMgr       *AtomClientManager
	ErrUnauthorized = errors.New("Unauthorized")
)

const (
	sessionIdName = "jwt_id"
)

type ClientInstance struct {
	id string
	*atom.Client
	t *time.Timer
}

// touch restarts the timeout timer with timeout d
func (o *ClientInstance) touch(d time.Duration, onTimeout func()) {
	o.stopTimer()
	o.t = time.AfterFunc(d, onTimeout)
}

// stopTimer stops the timeout timer
func (o *ClientInstance) stopTimer() {
	if o.t != nil {
		o.t.Stop()
	}
}

type AtomClientManager struct {
	mtx               sync.Mutex
	clients           map[string]*ClientInstance
	maxAge            time.Duration
	outRequestTimeout time.Duration
}

func InitClientManager(maxAge time.Duration, outRequestTimeout time.Duration) {
	if clientMgr != nil {
		panic("InitClientManager called twice")
	}

	clientMgr = &AtomClientManager{
		clients:           make(map[string]*ClientInstance),
		maxAge:            maxAge,
		outRequestTimeout: outRequestTimeout,
	}
}

func ClientManager() *AtomClientManager {
	return clientMgr
}

// Get returns an existing atom.Client
func (mgr *AtomClientManager) Get(w http.ResponseWriter, req *http.Request) (*ClientInstance, error) {
	cookie, err := req.Cookie(sessionIdName)
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if err == http.ErrNoCookie {
		return nil, ErrUnauthorized
	}
	inst, ok := mgr.clients[cookie.Value]
	if !ok {
		return nil, ErrUnauthorized
	}
	return inst, nil
}

// GetOrNew always returns a new atom.Client. If there is already an old atom.Client,
// StopQRLogin is called, then it is removed.
func (mgr *AtomClientManager) GetOrNew(w http.ResponseWriter, req *http.Request) (*ClientInstance, error) {
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
			Path:  "/",
		})
	}
	inst := &ClientInstance{id: id, Client: atom.NewClient()}
	inst.Client.SetTimeout(mgr.outRequestTimeout)
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

func RegisterHandlers(r *mux.Router) {
	r.HandleFunc("/api/startqrlogin", startQRLogin).Methods(http.MethodPost)
	r.HandleFunc("/api/isloggedin", isLoggedIn).Methods(http.MethodGet)
	r.HandleFunc("/api/getcommunities", getCommunities).Methods(http.MethodGet)
	r.HandleFunc("/api/setcurrentcommunity", setCurrentCommunity).Methods(http.MethodPost)
	r.HandleFunc("/api/like{kind:notices|moments|ccpposts|proposals}", likePosts).Methods(http.MethodPost)
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
		w.WriteHeader(http.StatusUnauthorized)
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
		w.WriteHeader(http.StatusUnauthorized)
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
		w.WriteHeader(http.StatusUnauthorized)
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
		w.WriteHeader(http.StatusUnauthorized)
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
