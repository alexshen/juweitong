package web

import (
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"text/template"

	"github.com/alexshen/juweitong/atom"
	"github.com/alexshen/juweitong/cmd/atom-server/api"
	"github.com/alexshen/juweitong/cmd/atom-server/dal"
	"github.com/gorilla/mux"
	"github.com/samber/lo"
)

var gHtmlRoot string
var gSelectedCommunitiesDAO dal.SelectedCommunitiesDAO

func Init(root string, selectedCommunitiesDAO dal.SelectedCommunitiesDAO) {
	gHtmlRoot = root
	gSelectedCommunitiesDAO = selectedCommunitiesDAO
}

func checkedExecute(t *template.Template, w http.ResponseWriter, data any) {
	if err := t.Execute(w, data); err != nil {
		log.Print(err)
	}
}

func RegisterHandlers(r *mux.Router) {
	r.HandleFunc("/qr_login", htmlQRLogin)
	r.HandleFunc("/community", htmlCommunity)
	r.HandleFunc("/dolike", htmlDoLike).Methods(http.MethodPost)
}

func getHtml(bodyFile string) *template.Template {
	page := template.Must(template.ParseFiles(filepath.Join(gHtmlRoot, "root.tmpl")))
	text, err := ioutil.ReadFile(filepath.Join(gHtmlRoot, bodyFile))
	if err != nil {
		panic(err)
	}
	template.Must(page.New("content").Parse(string(text)))
	return page
}

func htmlQRLogin(w http.ResponseWriter, r *http.Request) {
	t := getHtml("qr_login.tmpl")
	checkedExecute(t, w, nil)
}

func redirectQRLogin(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	data, err := ioutil.ReadFile(filepath.Join(gHtmlRoot, "redirect.html"))
	if err == nil {
		w.Write(data)
	}
}

func htmlCommunity(w http.ResponseWriter, r *http.Request) {
	type community struct {
		Name     string
		Selected bool
	}

	client := api.ClientManager().Get(api.GetSession(r))
	if client == nil {
		redirectQRLogin(w)
		return
	}
	selection, err := gSelectedCommunitiesDAO.FindAll(client.Id())
	if err != nil {
		log.Printf("failed to get selected communities: %v", err)
	}

	t := getHtml("community.tmpl")
	data := lo.Map(client.Communities(), func(e atom.Community, i int) community {
		return community{
			e.Name,
			lo.Contains(selection, e.Name),
		}
	})
	checkedExecute(t, w, data)
}

func htmlDoLike(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	client := api.ClientManager().Get(api.GetSession(r))
	if client == nil {
		redirectQRLogin(w)
		return
	}

	communities := r.Form["community"]
	if !lo.Every(
		lo.Map(client.Communities(),
			func(e atom.Community, i int) string { return e.Name }),
		communities) {
		http.Error(w, "invalid communities", http.StatusBadRequest)
		return
	}
	t := getHtml("dolike.tmpl")
	checkedExecute(t, w, communities)
}
