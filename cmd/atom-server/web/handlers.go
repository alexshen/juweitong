package web

import (
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"text/template"

	"github.com/alexshen/juweitong/atom"
	"github.com/alexshen/juweitong/cmd/atom-server/api"
	"github.com/gorilla/mux"
	"github.com/samber/lo"
)

var gHtmlRoot string

func SetHtmlRoot(root string) {
	gHtmlRoot = root
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
	client := api.ClientManager().Get(api.GetSession(r))
	if client == nil {
		redirectQRLogin(w)
		return
	}
	t := getHtml("community.tmpl")
	checkedExecute(t, w, client.Communites)
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
		lo.Map(client.Communites,
			func(e atom.Community, i int) string { return e.Name }),
		communities) {
		http.Error(w, "invalid communities", http.StatusBadRequest)
		return
	}
	t := getHtml("dolike.tmpl")
	checkedExecute(t, w, communities)
}
