package api

import (
	"log"
	"sync"
	"time"

	"github.com/alexshen/juweitong/atom"
	"github.com/alexshen/juweitong/cmd/atom-server/dal"
	"github.com/google/uuid"
	"github.com/gorilla/sessions"
)

const kKeyClientId = "api.client_id"

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
	likedPostsDAO     dal.LikedPostsDAO
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
	value, ok := session.Values[kKeyClientId]

	var id string
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
	dao := clientLikedPostsHistory{id, mgr.likedPostsDAO}
	inst := &ClientInstance{id: id, Client: atom.NewClient(&dao)}
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

var gClientMgr *AtomClientManager

func InitClientManager(maxAge time.Duration,
	outRequestTimeout time.Duration,
	likedPostsDAO dal.LikedPostsDAO) {
	if gClientMgr != nil {
		panic("InitClientManager called twice")
	}

	gClientMgr = &AtomClientManager{
		clients:           make(map[string]*ClientInstance),
		maxAge:            maxAge,
		outRequestTimeout: outRequestTimeout,
		likedPostsDAO:     likedPostsDAO,
	}
}
