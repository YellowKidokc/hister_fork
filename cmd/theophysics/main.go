// Theophysics is the orchestration API and desktop web control center.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/internal/research"
)

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func main() {
	token := os.Getenv("TP_ACCESS_TOKEN")
	dsn := os.Getenv("TP_DATABASE_URL")
	if len(token) < 24 || dsn == "" {
		log.Fatal("TP_DATABASE_URL and TP_ACCESS_TOKEN (at least 24 characters) are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
	store, err := research.Open(initCtx, dsn, value("TP_DATA_DIR", "./.theophysics-data"))
	initCancel()
	if err != nil {
		log.Fatal("Cannot initialize research storage; verify PostgreSQL, database permissions, and TP_DATA_DIR")
	}
	defer store.Close()
	ai := &research.Classifier{BaseURL: os.Getenv("TP_AI_BASE_URL"), Model: os.Getenv("TP_AI_MODEL"), Key: os.Getenv("TP_AI_KEY"), HTTP: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("AI endpoint redirects are not allowed") }}}
	api := &research.API{Store: store, Token: token, VolumeOneFolder: os.Getenv("TP_VOLUME_ONE_FOLDER"), Hister: client.New(value("TP_HISTER_URL", "http://127.0.0.1:4433"), client.WithAccessToken(os.Getenv("TP_HISTER_TOKEN")), client.WithTimeout(45*time.Second)), Classifier: ai, URLHosts: strings.FieldsFunc(os.Getenv("TP_URL_HOSTS"), func(r rune) bool { return r == ',' || r == ' ' })}
	server := &http.Server{Addr: value("TP_ADDRESS", "127.0.0.1:4444"), Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	go api.RunWorker(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("Theophysics control center listening on %s", server.Addr)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
