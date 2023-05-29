package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alexshen/juweitong/cmd/atom-server/api"
	"github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	myioutil "github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	"github.com/alexshen/juweitong/cmd/atom-server/web"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
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
	fAssetPath         = flag.String("asset", "", "root path to the assets")
	fHtmlPath          = flag.String("html", "", "root path to the html templates")
	fShutdownTimeout   = flag.Int("shutdown", 60, "graceful shutdown timeout in seconds")
)

var gLogFile *os.File

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

	if gLogFile != nil {
		gLogFile.Close()
	}
	gLogFile = f
	log.SetOutput(gLogFile)

	return nil
}

func main() {
	flag.Parse()

	store := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	store.MaxAge(0)

	router := mux.NewRouter()
	api.Init(store)
	api.InitClientManager(time.Second*time.Duration(*fMaxAge), time.Second*time.Duration(*fOutRequestTimeout))
	api.RegisterHandlers(router)

	// register assets handlers
	if *fAssetPath == "" {
		log.Fatal("asset root path not specified")
	}
	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir(*fAssetPath))))

	// register web handlers
	if *fHtmlPath == "" {
		log.Fatal("html root path not specified")
	}
	web.SetHtmlRoot(*fHtmlPath)
	web.RegisterHandlers(router)

	if err := reopenLogFile(); err != nil {
		log.Fatal(err)
	}
	logWriter := ioutil.NewRedirectableWriter(gLogFile)

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

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(*fShutdownTimeout))
		defer cancel()
		for {
			switch <-sigint {
			case os.Interrupt, syscall.SIGTERM:
				if err := server.Shutdown(ctx); err != nil {
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
				logWriter.SetWriter(gLogFile)
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
	api.ClientManager().Stop()

	log.Print("server has been shutdown")
	if gLogFile != nil {
		gLogFile.Close()
	}
}
