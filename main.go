// Copyright 2021 the fluxcdbot authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/julienschmidt/httprouter"
	"github.com/oklog/run"
	"github.com/peterbourgon/diskv/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/tucnak/telebot.v2"

	"github.com/squat/fluxcdbot/version"
)

const (
	logLevelAll   = "all"
	logLevelDebug = "debug"
	logLevelInfo  = "info"
	logLevelWarn  = "warn"
	logLevelError = "error"
	logLevelNone  = "none"
)

var (
	availableLogLevels = strings.Join([]string{
		logLevelAll,
		logLevelDebug,
		logLevelInfo,
		logLevelWarn,
		logLevelError,
		logLevelNone,
	}, ", ")
)

// Main is the principal function for the binary, wrapped only by `main` for convenience.
func Main() error {
	listen := flag.String("listen", ":8080", "The address at which to listen.")
	listenInternal := flag.String("listen-internal", ":9090", "The address at which to listen for health and metrics.")
	logLevel := flag.String("log-level", logLevelInfo, fmt.Sprintf("Log level to use. Possible values: %s", availableLogLevels))
	database := flag.String("database", "/var/fluxcdbot", "The path to the directory to use for the database.")
	tmpDir := flag.String("tmp", "/var/fluxcdbot/tmp", "The path to a directory to use for temporary storage.")
	token := flag.String("token", "", "The Telegram API token.")
	baseURLRaw := flag.String("url", "http://127.0.0.1:8080", "The URL clients should use to commincate with this server.")
	printVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *printVersion {
		fmt.Println(version.Version)
		return nil
	}

	logger := log.NewJSONLogger(log.NewSyncWriter(os.Stdout))
	switch *logLevel {
	case logLevelAll:
		logger = level.NewFilter(logger, level.AllowAll())
	case logLevelDebug:
		logger = level.NewFilter(logger, level.AllowDebug())
	case logLevelInfo:
		logger = level.NewFilter(logger, level.AllowInfo())
	case logLevelWarn:
		logger = level.NewFilter(logger, level.AllowWarn())
	case logLevelError:
		logger = level.NewFilter(logger, level.AllowError())
	case logLevelNone:
		logger = level.NewFilter(logger, level.AllowNone())
	default:
		return fmt.Errorf("log level %v unknown; possible values are: %s", *logLevel, availableLogLevels)
	}
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.DefaultCaller)

	r := prometheus.NewRegistry()
	r.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

	baseURL, err := url.Parse(*baseURLRaw)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	d := diskv.New(diskv.Options{
		BasePath: *database,
		TempDir:  *tmpDir,
	})

	b, err := telebot.NewBot(telebot.Settings{
		ParseMode: telebot.ModeMarkdownV2,
		Poller:    &telebot.LongPoller{Timeout: 10 * time.Second},
		Token:     *token},
	)
	if err != nil {
		return fmt.Errorf("failed to create Telegram API client: %w", err)
	}

	var g run.Group
	{
		b.Handle("/start", handleStart(d, b, baseURL, logger))
		b.Handle("/rotate", handleRotate(d, b, baseURL, logger))
		// Run the Telegram bot.
		g.Add(func() error {
			b.Start()
			return nil
		}, func(error) {
			b.Stop()
		})
	}

	{
		// Run the internal HTTP server.
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))
		l, err := net.Listen("tcp", *listenInternal)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %v", *listenInternal, err)
		}

		g.Add(func() error {
			if err := http.Serve(l, mux); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("error: internal server exited unexpectedly: %v", err)
			}
			return nil
		}, func(error) {
			l.Close()
		})
	}

	{
		// Run the HTTP server.
		router := httprouter.New()
		router.HandlerFunc(http.MethodPost, path.Join(webhookEndpoint, "/:chatID/:uuid"), handleWebhook(d, b))
		l, err := net.Listen("tcp", *listen)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %v", *listen, err)
		}

		g.Add(func() error {
			if err := http.Serve(l, router); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("error: server exited unexpectedly: %v", err)
			}
			return nil
		}, func(error) {
			l.Close()
		})
	}

	{
		// Exit gracefully on SIGINT and SIGTERM.
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGINT, syscall.SIGTERM)
		cancel := make(chan struct{})
		g.Add(func() error {
			for {
				select {
				case <-term:
					logger.Log("msg", "caught interrupt; gracefully cleaning up; see you next time!")
					return nil
				case <-cancel:
					return nil
				}
			}
		}, func(error) {
			close(cancel)
		})
	}

	return g.Run()
}

func main() {
	if err := Main(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
