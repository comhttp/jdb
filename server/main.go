package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/comhttp/jdb"
	"github.com/dgraph-io/badger/v3"
	daemon "github.com/leprosus/golang-daemon"
	"github.com/rs/zerolog"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func wrapLogger(module string) logrus.FieldLogger {
	return log.WithField("module", module)
}

func parseLogLevel(level string) logrus.Level {
	switch level {
	case "error":
		return logrus.ErrorLevel
	case "warn", "warning":
		return logrus.WarnLevel
	case "info", "notice":
		return logrus.InfoLevel
	case "debug":
		return logrus.DebugLevel
	case "trace":
		return logrus.TraceLevel
	default:
		return logrus.InfoLevel
	}
}

func main() {
	// Get cmd line parameters
	bind := flag.String("bind", "localhost:4338", "HTTP server bind in format addr:port")
	dbfile := flag.String("dbdir", "data", "Path to jdb database dir")
	loglevel := flag.String("loglevel", "info", "Logging level (debug, info, warn, error)")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Default level for this example is info, unless debug flag is present

	switch *loglevel {
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}

	err := daemon.Init(os.Args[0], map[string]interface{}{}, *dbfile+"/daemonized.pid")
	if err != nil {
		return
	}

	switch os.Args[1] {
	case "start":
		err = daemon.Start()
	case "stop":
		err = daemon.Stop()
	case "restart":
		err = daemon.Stop()
		err = daemon.Start()
	case "status":
		status := "stopped"
		if daemon.IsRun() {
			status = "started"
		}

		fmt.Printf("Application is %s\n", status)

		return
	case "":
	default:
		MainLoop(*dbfile, *bind)
		fmt.Println("JORM node is on: :" + *bind)
	}
}

func MainLoop(dbfile, bind string) {
	// Loading routine
	options := badger.DefaultOptions(dbfile)
	options.Logger = wrapLogger("db")
	db, err := badger.Open(options)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Initialize KV (required)
	hub, err := jdb.NewHub(db, wrapLogger("jdb"))
	if err != nil {
		panic(err)
	}
	go hub.Run()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		jdb.ServeWs(hub, w, r)
	})

	// Start HTTP server
	http.ListenAndServe(bind, nil)
}
