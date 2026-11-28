package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/api"
	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/TeemoSun/mihomo-deck/internal/config"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
)

func main() {
	health := flag.Bool("healthcheck", false, "probe local health endpoint")
	flag.Parse()
	if *health {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + port + "/api/health")
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("health check: %s", resp.Status)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(cfg.TZ)
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	time.Local = loc
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		return err
	}
	empty, err := database.UsersEmpty(db)
	if err != nil {
		return err
	}
	if err = cfg.ValidateSecrets(empty); err != nil {
		return err
	}
	if err = database.EnsureAdminSeed(db, cfg.AuthUsername, cfg.AuthPassword); err != nil {
		return err
	}
	prefs, err := database.GetPreferences(db, database.DefaultPreferences(cfg.RetentionDays, cfg.SnapshotIntervalMin, cfg.LogPersistLevel))
	if err != nil {
		return err
	}
	client := mihomo.NewClient(cfg.MihomoURL, cfg.MihomoSecret)
	c := collector.New(db, client, prefs)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c.Start(ctx)
	handler := api.NewRouter(db, cfg, client, c)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err = <-errs:
		if err != http.ErrServerClosed {
			handler.CloseAllSockets()
			c.Stop()
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	handler.CloseAllSockets()
	err = server.Shutdown(shutdown)
	c.Stop()
	return err
}
