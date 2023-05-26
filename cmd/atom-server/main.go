package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	myioutil "github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

var (
	fPort              = flag.Int("port", 8080, "listening port")
	fMaxAge            = flag.Int("age", 600, "max age of a session in second")
	fHttp              = flag.Bool("http", false, "run in http mode")
	fCABundle          = flag.String("ca", "", "path to the ca bundle file")
	fCert              = flag.String("cert", "", "path to the cert file")
	fPrivateKey        = flag.String("key", "", "path to the private key")
	fLog               = flag.String("log", "", "path to the log file, if empty, logging to os.Stdout")
	fOutRequestTimeout = flag.Int("timeout", 60, "seconds before an outgoing request times out")
)

var logFile *os.File

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

// getCertFile returns the file path containing the cert file and ca bundle
func getCertFile(caBundlePath, certPath string) (string, error) {
	f, err := os.CreateTemp("", "atom-server-cert")
	if err != nil {
		return "", err
	}
	defer f.Close() // ignore
	if err := myioutil.ConcatFiles(f, certPath, caBundlePath); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func reopenLogFile() error {
	if *fLog == "" {
		return nil
	}
	f, err := os.OpenFile(*fLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if logFile != nil {
		logFile.Close()
	}
	logFile = f
	log.SetOutput(logFile)

	return nil
}

func main() {
	flag.Parse()
	clientMgr = NewAtomClientManager(time.Second * time.Duration(*fMaxAge))
	router := mux.NewRouter()

	// register apis
	router.HandleFunc("api/startqrlogin", startQRLogin).Methods("POST")
	router.HandleFunc("api/isloggedin", isLoggedIn).Methods("GET")
	router.HandleFunc("api/getcommunities", getCommunities).Methods("GET")
	router.HandleFunc("api/setcurrentcommunity", setCurrentCommunity).Methods("POST")
	router.HandleFunc("api/like{kind:notices|moments|ccpposts|proposals}", likePosts).Methods("POST")

	if err := reopenLogFile(); err != nil {
		log.Fatal(err)
	}
	logWriter := ioutil.NewRedirectableWriter(logFile)

	server := http.Server{
		Addr:         ":" + strconv.Itoa(*fPort),
		Handler:      handlers.LoggingHandler(logWriter, router),
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
	}
	log.Printf("starting server, listening at %s", server.Addr)

	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

		for {
			switch <-sigint {
			case os.Interrupt, syscall.SIGTERM:
				if err := server.Shutdown(context.Background()); err != nil {
					log.Printf("Shutdown: %v", err)
				}
				return
			case syscall.SIGHUP:
				// reopen the log file
				if err := reopenLogFile(); err != nil {
					log.Printf("unable to open log file: %v", err)
					break
				}
				log.Printf("log file reopened")
				logWriter.SetWriter(logFile)
			}
		}
	}()

	if *fHttp {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	} else {
		certFile, err := getCertFile(*fCABundle, *fCert)
		if err != nil {
			log.Fatalf("failed to create the cert file: %v", err)
		}
		defer os.Remove(certFile)
		if err := server.ListenAndServeTLS(certFile, *fPrivateKey); err != http.ErrServerClosed {
			log.Fatalf("ListenAndServeTLS: %v", err)
		}
	}

	<-shutdown
	clientMgr.Stop()

	log.Print("server has been shutdown")
	if logFile != nil {
		logFile.Close()
	}
}
