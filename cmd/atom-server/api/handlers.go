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
	"github.com/gorilla/sessions"
	"github.com/samber/lo"
)

var ErrUnauthorized = errors.New("Unauthorized")

var (
	gClientMgr *AtomClientManager
	gStore     sessions.Store
)

const (
	kKeyClientId = "api.client_id"
	kSessionName = "api.session"
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

func Init(store sessions.Store) {
	gStore = store
}

func GetSession(r *http.Request) *sessions.Session {
	session, _ := gStore.Get(r, kSessionName)
	return session
}

func InitClientManager(maxAge time.Duration, outRequestTimeout time.Duration) {
	if gClientMgr != nil {
		panic("InitClientManager called twice")
	}

	gClientMgr = &AtomClientManager{
		clients:           make(map[string]*ClientInstance),
		maxAge:            maxAge,
		outRequestTimeout: outRequestTimeout,
	}
}

func ClientManager() *AtomClientManager {
	return gClientMgr
}

// Get returns an existing atom.Client.
func (mgr *AtomClientManager) Get(session *sessions.Session) *ClientInstance {
	value, ok := session.Values[kKeyClientId]
	if !ok {
		return nil
	}
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	inst, _ := mgr.clients[value.(string)]
	return inst
}

// New returns a new atom.Client.
func (mgr *AtomClientManager) New(session *sessions.Session) (*ClientInstance, error) {
	var id string
	value, ok := session.Values[kKeyClientId]

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()
	if ok {
		id = value.(string)
		mgr.removeNoLock(id)
	} else {
		newId, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		id = newId.String()
		session.Values[kKeyClientId] = id
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
	mgr.removeNoLock(id)
}

func (mgr *AtomClientManager) removeNoLock(id string) {
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
	r.HandleFunc("/api/getcommunities", ensureLoggedIn(getCommunities)).Methods(http.MethodGet)
	r.HandleFunc("/api/setcurrentcommunity", ensureLoggedIn(setCurrentCommunity)).Methods(http.MethodPost)
	r.HandleFunc("/api/like{kind:notices|moments|ccpposts|proposals}", ensureLoggedIn(likePosts)).Methods(http.MethodPost)
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

	session, _ := gStore.Get(r, kSessionName)
	client, err := gClientMgr.New(session)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
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
	if err := session.Save(r, w); err != nil {
		log.Printf("session save failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeSuccess(w, responseData{qrcodeUrl})
}

func isLoggedIn(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		LoggedIn bool `json:"loggedin"`
	}

	session, _ := gStore.Get(r, kSessionName)
	client := gClientMgr.Get(session)
	if client == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	writeSuccess(w, responseData{client.IsLoggedIn()})
}

type apiMustLoggedInFunc func(w http.ResponseWriter, r *http.Request, client *ClientInstance)

func ensureLoggedIn(next apiMustLoggedInFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := gStore.Get(r, kSessionName)
		client := gClientMgr.Get(session)
		if client == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if !client.IsLoggedIn() {
			http.Error(w, "not logged in", http.StatusBadRequest)
			return
		}
		next(w, r, client)
	}
}

func getCommunities(w http.ResponseWriter, r *http.Request, client *ClientInstance) {
	type responseData struct {
		Names   []string `json:"names"`
		Current int      `json:"current"`
	}

	writeSuccess(w, responseData{
		Names: lo.Map(client.Communities(), func(e atom.Community, i int) string {
			return e.Name
		}),
		Current: client.CurrentCommunityIndex(),
	})
}

func setCurrentCommunity(w http.ResponseWriter, r *http.Request, client *ClientInstance) {
	type requestData struct {
		Current int    `json:"current,omitempty"`
		Name    string `json:"name"`
	}

	query := requestData{Current: -1}
	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		log.Printf("invalid query: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	index := query.Current
	if query.Current == -1 {
		_, index, _ = lo.FindIndexOf(client.Communities(), func(e atom.Community) bool { return e.Name == query.Name })
		if index == -1 {
			log.Printf("invalid community name: %s", query.Name)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else if query.Current < 0 || query.Current >= len(client.Communities()) {
		log.Printf("invalid community index: %d", query.Current)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := client.SetCurrentCommunity(index); err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, nil)
}

func likePosts(w http.ResponseWriter, r *http.Request, client *ClientInstance) {
	type responseData struct {
		Count int `json:"count"`
	}

	type requestData responseData

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
