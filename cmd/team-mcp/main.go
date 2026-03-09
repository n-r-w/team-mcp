// Package main starts the Team MCP server binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/n-r-w/team-mcp/internal/appinit"
	"github.com/n-r-w/team-mcp/internal/config"
)

const defaultBuildVersion = "dev"

// main starts Team MCP server lifecycle.
func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "team-mcp failed: %v\n", err)
		os.Exit(1)
	}
}

// run loads config, wires dependencies, and serves MCP transport.
func run() error {
	showVersion := flag.Bool("version", false, "print build version and exit")
	flag.Parse()

	if *showVersion {
		_, _ = fmt.Fprintln(os.Stdout, defaultBuildVersion)

		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	application, err := appinit.New(cfg, defaultBuildVersion)
	if err != nil {
		return fmt.Errorf("build app service: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runErr := application.Run(ctx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	return nil
}
