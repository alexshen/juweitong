package api

import (
	"encoding/json"
	"net/http"

	"github.com/alexshen/juweitong/atom"
	"github.com/alexshen/juweitong/cmd/atom-server/dal"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/op/go-logging"
	"github.com/samber/lo"
)

var (
	gStore                  sessions.Store
	gSelectedCommunitiesDAO dal.SelectedCommunitiesDAO
	gLog                    = logging.MustGetLogger("api")
)

const kSessionName = "api.session"

func Init(store sessions.Store, selectedCommunitiesDAO dal.SelectedCommunitiesDAO) {
	gStore = store
	gSelectedCommunitiesDAO = selectedCommunitiesDAO
}

func GetSession(r *http.Request) *sessions.Session {
	session, _ := gStore.Get(r, kSessionName)
	return session
}

type clientLikedPostsHistory struct {
	clientId string
	dao      dal.LikedPostsDAO
}

func (o *clientLikedPostsHistory) Has(post atom.LikedPost) (bool, error) {
	return o.dao.Has(dal.LikedPost{
		MemberId: post.MemberId,
		PostId:   post.PostId,
	})
}

func (o *clientLikedPostsHistory) Add(post atom.LikedPost) error {
	return o.dao.Add(dal.LikedPost{
		MemberId: post.MemberId,
		PostId:   post.PostId,
	})
}

func RegisterHandlers(r *mux.Router) {
	r.HandleFunc("/api/startqrlogin", startQRLogin).Methods(http.MethodPost)
	r.HandleFunc("/api/isloggedin", isLoggedIn).Methods(http.MethodGet)
	r.HandleFunc("/api/getcommunities", ensureLoggedIn(getCommunities)).Methods(http.MethodGet)
	r.HandleFunc("/api/selectcommunities", ensureLoggedIn(selectCommunities)).Methods(http.MethodPost)
	r.HandleFunc("/api/setcurrentcommunity", ensureLoggedIn(setCurrentCommunity)).Methods(http.MethodPost)
	r.HandleFunc("/api/like{kind:notices|moments|ccpposts|proposals}", ensureLoggedIn(likePosts)).Methods(http.MethodPost)
}

func startQRLogin(w http.ResponseWriter, r *http.Request) {
	type responseData struct {
		Url string `json:"url"`
	}

	session, _ := gStore.Get(r, kSessionName)
	client, err := gClientMgr.New(session)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		gLog.Error(err)
		return
	}
	gLog.Infof("start qr login for %s", client.id)
	qrcodeUrl, err := client.StartQRLogin(func() {
		gLog.Infof("%s logged in", client.id)
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if err := session.Save(r, w); err != nil {
		gLog.Errorf("session save failed: %v", err)
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

type community struct {
	MemberId string `json:"member_id"`
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
}

func getCommunities(w http.ResponseWriter, r *http.Request, client *ClientInstance) {
	type responseData struct {
		Communties []community `json:"communities"`
		Current    string      `json:"current"`
	}

	selection, err := gSelectedCommunitiesDAO.FindAll(client.Id())
	if err != nil {
		gLog.Errorf("failed to get selected communities: %v", err)
	}
	writeSuccess(w, responseData{
		Communties: lo.Map(client.Communities(), func(e atom.Community, i int) community {
			return community{
				MemberId: e.MemberId,
				Name:     e.Name,
				Selected: lo.Contains(selection, e.MemberId),
			}
		}),
		Current: client.CurrentCommunity().MemberId,
	})
}

func selectCommunities(w http.ResponseWriter, r *http.Request, client *ClientInstance) {
	var requestData = struct {
		Communities []community `json:"communities"`
	}{}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		gLog.Errorf("invalid query: %v", err)
		http.Error(w, "invalid arguments", http.StatusBadRequest)
		return
	}

	for _, c := range requestData.Communities {
		if !lo.ContainsBy(client.Communities(), func(e atom.Community) bool {
			return e.MemberId == c.MemberId
		}) {
			gLog.Errorf("invalid community: %s", c.Name)
			continue
		}
		r := dal.SelectedCommunity{UserId: client.Id(), MemberId: c.MemberId}
		if c.Selected {
			if _, err := gSelectedCommunitiesDAO.Add(r); err != nil {
				gLog.Errorf("failed to insert selected community: %v", err)
			}
		} else {
			if err := gSelectedCommunitiesDAO.Delete(r); err != nil {
				gLog.Errorf("failed to remove selected community: %v", err)
			}
		}
	}
	writeSuccess(w, nil)
}

func setCurrentCommunity(w http.ResponseWriter, r *http.Request, client *ClientInstance) {
	var requestData = struct {
		MemberId string `json:"member_id"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		gLog.Errorf("invalid query: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := client.SetCurrentCommunityById(requestData.MemberId); err != nil {
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
		gLog.Errorf("invalid query: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if query.Count <= 0 {
		gLog.Errorf("invalid count %d", query.Count)
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
		gLog.Errorf("unhandled like kind: %s", kind)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	writeSuccess(w, responseData{numPosts})
}
