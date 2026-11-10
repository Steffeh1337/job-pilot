// Command server polls job boards, classifies listings and serves the API.
//
//	server [-config config.yaml] serve        poll on a schedule and serve the API (default)
//	server [-config config.yaml] once         poll every source now, then print the funnel
//	server [-config config.yaml] funnel [N]   print the funnel for the last N days (default 7)
//	server [-config config.yaml] reclassify   re-run filters over stored jobs, then print the funnel
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/httpapi"
	"github.com/Steffeh1337/job-pilot/backend/internal/pipeline"
	"github.com/Steffeh1337/job-pilot/backend/internal/scheduler"
	"github.com/Steffeh1337/job-pilot/backend/internal/source"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "config.yaml", "path to the YAML config (defaults apply if missing)")
	flag.Parse()
	cmd := flag.Arg(0)
	if cmd == "" {
		cmd = "serve"
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	filter, err := pipeline.NewFilter(cfg.Filter)
	if err != nil {
		return err
	}
	pipe := pipeline.New(filter, st)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sched := &scheduler.Scheduler{
		Sources:  source.Build(cfg, source.NewClient(cfg.Contact)),
		Pipeline: pipe, Store: st, Tick: cfg.Tick, Log: log,
	}

	switch cmd {
	case "serve":
		return serve(ctx, cfg, st, sched, log)
	case "once":
		sched.RunOnce(ctx)
		return printFunnel(ctx, st, 7)
	case "funnel":
		days := 7
		if a := flag.Arg(1); a != "" {
			if days, err = strconv.Atoi(a); err != nil || days < 1 {
				return fmt.Errorf("funnel: %q is not a number of days", a)
			}
		}
		return printFunnel(ctx, st, days)
	case "reclassify":
		n, err := pipe.Reclassify(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%d jobs changed stage\n\n", n)
		return printFunnel(ctx, st, 36500)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func serve(ctx context.Context, cfg config.Config, st *store.Store, sched *scheduler.Scheduler, log *slog.Logger) error {
	api := &httpapi.API{Store: st, Log: log}
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}

	go sched.Run(ctx)

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("serving", "addr", cfg.HTTPAddr, "sources", len(sched.Sources), "tick", cfg.Tick)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
