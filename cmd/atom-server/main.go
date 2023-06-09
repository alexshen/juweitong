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
	"github.com/alexshen/juweitong/cmd/atom-server/dal"
	"github.com/alexshen/juweitong/cmd/atom-server/ioutil"
	"github.com/alexshen/juweitong/cmd/atom-server/web"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/op/go-logging"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type loggingLevel struct {
	level logging.Level
}

func (l *loggingLevel) String() string {
	return l.level.String()
}

func (l *loggingLevel) Set(s string) error {
	level, err := logging.LogLevel(s)
	if err != nil {
		return err
	}
	l.level = level
	return nil
}

var (
	fPort              = flag.Int("port", 8080, "listening port")
	fMaxAge            = flag.Int("age", 600, "max age of a session in second")
	fHttp              = flag.Bool("http", false, "run in http mode")
	fCABundle          = flag.String("ca", "", "path to the ca bundle file")
	fCert              = flag.String("cert", "", "path to the cert file")
	fPrivateKey        = flag.String("key", "", "path to the private key")
	fServerLog         = flag.String("serverlog", "server.log", "path to the server log file, if empty, logging to os.Stdout")
	fAccessLog         = flag.String("accesslog", "access.log", "path to the access log file, if empty, logging to os.Stdout")
	fOutRequestTimeout = flag.Int("timeout", 60, "seconds before an outgoing request times out")
	fAssetPath         = flag.String("asset", "", "root path to the assets")
	fHtmlPath          = flag.String("html", "", "root path to the html templates")
	fShutdownTimeout   = flag.Int("shutdown", 60, "graceful shutdown timeout in seconds")
	fDBPath            = flag.String("db", "", "path to the sqlite3 database")
	fLogLevel          loggingLevel
)

var gLog = logging.MustGetLogger("main")

func init() {
	fLogLevel.level = logging.INFO
	flag.Var(&fLogLevel, "level", "logging level, valid values are DEBUG,INFO,WARN,ERROR")
}

// getCertFile returns the file path containing the cert file and ca bundle
func getCertFile(caBundlePath, certPath string) (string, error) {
	f, err := os.CreateTemp("", "atom-server-cert")
	if err != nil {
		return "", err
	}
	defer f.Close() // ignore
	if err := ioutil.ConcatFiles(f, certPath, caBundlePath); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// openLogFile closes the old log file and open a new log file for appending.
// If path is empty, the old log is simply reopend.
func mustOpenLogFile(old *os.File, path string) *os.File {
	if old != nil {
		if err := old.Close(); err != nil {
			log.Fatal("failed to close log: ", old.Name())
		}
	}
	if path == "" {
		if old == nil {
			panic("path is empty but old is not given")
		}
		path = old.Name()
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal("failed to open log: ", path)
	}
	return f
}

func main() {
	flag.Parse()

	var serverLogFile *os.File
	serverLogWriter := os.Stdout
	if *fServerLog != "" {
		serverLogFile = mustOpenLogFile(nil, *fServerLog)
		serverLogWriter = serverLogFile
	}

	var accessLogFile *os.File
	accessLogWriter := ioutil.NewRedirectableWriter(os.Stdout)
	if *fAccessLog != "" {
		accessLogFile = mustOpenLogFile(nil, *fAccessLog)
		accessLogWriter.SetWriter(accessLogFile)
	}
	initLogging(serverLogWriter, fLogLevel.level)

	store := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	store.MaxAge(0)
	store.Options.Secure = !*fHttp

	var likedPostsDAO dal.LikedPostsDAO
	var selectedCommunitiesDAO dal.SelectedCommunitiesDAO
	if *fDBPath != "" {
		gLog.Infof("using db at path %s", *fDBPath)
		db, err := gorm.Open(sqlite.Open(*fDBPath), &gorm.Config{})
		if err != nil {
			gLog.Fatal(err)
		}
		if err := db.AutoMigrate(&dal.LikedPost{}, &dal.SelectedCommunity{}); err != nil {
			gLog.Fatal(err)
		}
		likedPostsDAO = dal.NewDBLikedPostsDAO(db)
		selectedCommunitiesDAO = dal.NewSelectedCommunitiesDAO(db)
	} else {
		gLog.Info("running without using db")
		likedPostsDAO = dal.NullLikedPostsDAO{}
		selectedCommunitiesDAO = dal.NullSelectedCommunitiesDAO{}
	}

	router := mux.NewRouter()
	api.Init(store, selectedCommunitiesDAO)
	api.InitClientManager(time.Second*time.Duration(*fMaxAge),
		time.Second*time.Duration(*fOutRequestTimeout),
		likedPostsDAO)
	api.RegisterHandlers(router)

	// register assets handlers
	if *fAssetPath == "" {
		gLog.Fatal("asset root path not specified")
	}
	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir(*fAssetPath))))

	// register web handlers
	if *fHtmlPath == "" {
		gLog.Fatal("html root path not specified")
	}
	web.Init(*fHtmlPath, selectedCommunitiesDAO)
	web.RegisterHandlers(router)

	server := http.Server{
		Addr:         ":" + strconv.Itoa(*fPort),
		Handler:      handlers.LoggingHandler(accessLogWriter, router),
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
	}
	gLog.Infof("starting server, listening at %s", server.Addr)

	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

		for {
			switch <-sigint {
			case os.Interrupt, syscall.SIGTERM:
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(*fShutdownTimeout))
				defer cancel()
				if err := server.Shutdown(ctx); err != nil {
					gLog.Errorf("Shutdown: %v", err)
				}
				return
			case syscall.SIGHUP:
				if serverLogFile != nil {
					serverLogFile = mustOpenLogFile(serverLogFile, "")
					setLogWriter(serverLogFile)
					gLog.Info("reopened log file:", serverLogFile.Name())
				}

				if accessLogFile != nil {
					accessLogFile = mustOpenLogFile(accessLogFile, "")
					accessLogWriter.SetWriter(accessLogFile)
					gLog.Info("reopened log file:", accessLogFile.Name())
				}
			}
		}
	}()

	if *fHttp {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			gLog.Fatalf("ListenAndServe: %v", err)
		}
	} else {
		certFile, err := getCertFile(*fCABundle, *fCert)
		if err != nil {
			gLog.Fatalf("failed to create the cert file: %v", err)
		}
		defer os.Remove(certFile)
		if err := server.ListenAndServeTLS(certFile, *fPrivateKey); err != http.ErrServerClosed {
			gLog.Fatalf("ListenAndServeTLS: %v", err)
		}
	}

	<-shutdown
	api.ClientManager().Stop()

	gLog.Info("server has been shutdown")
	uninitLogging()

	if serverLogFile != nil {
		serverLogFile.Close()
	}
	if accessLogFile != nil {
		accessLogFile.Close()
	}
}
