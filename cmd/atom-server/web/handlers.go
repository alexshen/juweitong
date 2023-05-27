package web

import (
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"text/template"

	"github.com/alexshen/juweitong/cmd/atom-server/api"
	"github.com/gorilla/mux"
)

var gHtmlRoot string

func SetHtmlRoot(root string) {
	gHtmlRoot = root
}

func RegisterHandlers(r *mux.Router) {
	r.HandleFunc("/qr_login", htmlQRLogin)
	r.HandleFunc("/community", htmlCommunity)
	r.HandleFunc("/dolike", htmlDoLike).Methods("POST")
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
	t.Execute(w, nil)
}

func redirectQRLogin(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	data, err := ioutil.ReadFile(filepath.Join(gHtmlRoot, "redirect.html"))
	if err == nil {
		w.Write(data)
	}
}

func htmlCommunity(w http.ResponseWriter, r *http.Request) {
	client, err := api.ClientManager().Get(w, r)
	if err == api.ErrUnauthorized {
		redirectQRLogin(w)
		return
	}
	if err != nil {
		log.Print(err)
		return
	}
	t := getHtml("community.tmpl")
	t.Execute(w, client.Communites)
}

func htmlDoLike(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	communities := r.Form["community"]
	t := getHtml("dolike.tmpl")
	t.Execute(w, communities)
}
