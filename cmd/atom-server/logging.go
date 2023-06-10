package main

import (
	"io"
	"os"

	"github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	"github.com/op/go-logging"
)

var (
	gLogWriter *ioutil.RedirectableWriter
)

func initLogging(w io.Writer, level logging.Level) {
	var format = logging.MustStringFormatter(
		`%{color}%{time:2006-01-02 15:04:05.000} %{level:.4s} %{shortfunc}%{color:reset} - %{message}`)
	logging.SetFormatter(format)
	logging.SetLevel(level, "")
	gLogWriter = ioutil.NewRedirectableWriter(w)
	backend := logging.NewLogBackend(gLogWriter, "", 0)
	backend.Color = w == os.Stdout
	logging.SetBackend(backend)
}

func setLogWriter(w io.Writer) {
	gLogWriter.SetWriter(w)
}

func uninitLogging() {
	gLogWriter = nil
}
