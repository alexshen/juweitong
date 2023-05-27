package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"text/template"
)

func getHtml(bodyFile string) *template.Template {
	page := template.Must(template.ParseFiles(filepath.Join(*fHtmlPath, "root.tmpl")))
	text, err := ioutil.ReadFile(filepath.Join(*fHtmlPath, bodyFile))
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
	data, err := ioutil.ReadFile(filepath.Join(*fHtmlPath, "redirect.html"))
	if err == nil {
		w.Write(data)
	}
}

func htmlCommunity(w http.ResponseWriter, r *http.Request) {
	client, err := clientMgr.Get(w, r)
	if err == errUnauthorized {
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
