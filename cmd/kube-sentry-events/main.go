package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/imankulov/kube-sentry-events/internal/config"
	"github.com/imankulov/kube-sentry-events/internal/dedup"
	"github.com/imankulov/kube-sentry-events/internal/filter"
	"github.com/imankulov/kube-sentry-events/internal/sentry"
	"github.com/imankulov/kube-sentry-events/internal/watcher"
)

var (
	version = "dev"
)

func main() {
	// CLI flags
	var (
		dryRun     = flag.Bool("dry-run", false, "Print events to stdout instead of sending to Sentry")
		kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file (defaults to in-cluster config or ~/.kube/config)")
		once       = flag.Bool("once", false, "List matching events once and exit (don't watch)")
		showVer    = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("kube-sentry-events %s\n", version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*dryRun)
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Set up logger
	logger := setupLogger(cfg.LogLevel, *dryRun)

	logger.Info("starting kube-sentry-events",
		"version", version,
		"dry_run", *dryRun,
		"once", *once,
		"environment", cfg.SentryEnvironment,
		"namespaces", cfg.Namespaces,
		"exclude_namespaces", cfg.ExcludeNamespaces,
		"event_reasons", cfg.EventReasons,
		"dedup_window", cfg.DedupWindow,
	)

	// Initialize sender (Sentry or stdout)
	var sender watcher.EventSender
	var sentrySender *sentry.Sender
	if *dryRun {
		sender = sentry.NewDryRunSender(os.Stdout)
		logger.Info("dry-run mode enabled, events will be printed to stdout")
	} else {
		var err error
		sentrySender, err = sentry.New(cfg.SentryDSN, cfg.SentryEnvironment, cfg.EnableLogs)
		if err != nil {
			logger.Error("failed to initialize Sentry", "error", err)
			os.Exit(1)
		}
		sender = sentrySender
		if cfg.EnableLogs {
			logger.Info("Sentry Logs enabled - all events will be logged for observability")
		}
	}

	// Initialize filter
	eventFilter := filter.New(cfg.Namespaces, cfg.ExcludeNamespaces, cfg.EventReasons, cfg.EventThresholds)

	// Initialize deduplicator
	deduplicator := dedup.New(cfg.DedupWindow)

	// Initialize watcher
	eventWatcher, err := watcher.New(eventFilter, deduplicator, sender, logger, *kubeconfig)
	if err != nil {
		logger.Error("failed to create watcher", "error", err)
		os.Exit(1)
	}

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run in appropriate mode
	if *once {
		if err := eventWatcher.ListOnce(ctx); err != nil {
			logger.Error("list error", "error", err)
			os.Exit(1)
		}
	} else {
		if err := eventWatcher.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("watcher error", "error", err)
			os.Exit(1)
		}
	}

	// Flush Sentry events before exit
	if sentrySender != nil {
		logger.Info("flushing events to Sentry...")
		if ok := sentrySender.Flush(5 * time.Second); ok {
			logger.Info("all events flushed successfully")
		} else {
			logger.Warn("some events may not have been sent (flush timeout)")
		}
	}

	logger.Info("shutdown complete")
}

func setupLogger(level string, humanReadable bool) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	var handler slog.Handler
	if humanReadable {
		// Use text handler for local development
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		})
	} else {
		// Use JSON handler for production
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	}
	return slog.New(handler)
}
