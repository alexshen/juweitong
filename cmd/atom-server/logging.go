package main

import (
	"io"
	"os"

	"github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	"github.com/op/go-logging"
)

var (
	gLogFile   *os.File
	gLogWriter *ioutil.RedirectableWriter
	gLog       *logging.Logger = logging.MustGetLogger("main")
)

func initLogging(filename string, level logging.Level) (io.Writer, error) {
	if filename != "" {
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		gLogFile = f
	}

	var format = logging.MustStringFormatter(
		`%{color}%{time:15:04:05.000} %{level:.4s} %{shortfunc}%{color:reset} %{message}`)
	logging.SetFormatter(format)
	logging.SetLevel(level, "")
	var w io.Writer = os.Stdout
	if gLogFile != nil {
		w = gLogFile
	}
	gLogWriter = ioutil.NewRedirectableWriter(w)
	backend := logging.NewLogBackend(gLogWriter, "", 0)
	backend.Color = gLogFile == nil
	logging.SetBackend(backend)
	return gLogWriter, nil
}

func uninitLogging() {
	if gLogFile != nil {
		gLogFile.Close()
		gLogFile = nil
	}
	gLog = nil
	gLogWriter = nil
}

func reopenLogFile() error {
	if gLogFile == nil {
		return nil
	}
	f, err := os.OpenFile(gLogFile.Name(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if err := gLogFile.Close(); err != nil {
		gLog.Warningf("failed to close the old log file: %v", err)
	}
	gLogFile = f
	gLogWriter.SetWriter(gLogFile)

	return nil
}
