package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/gops/agent"
	"github.com/scott-cotton/cli"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	"github.com/signadot/tony-format/go-tony/system/docd/txpool"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type UpConfig struct {
	*MainConfig
	Up       *cli.Command
	DataDir  string `cli:"name=data desc='data directory for logd storage'"`
	LogdAddr string `cli:"name=logd-addr desc='logd listen address' default=localhost:9123"`
	DocdAddr string `cli:"name=docd-addr desc='docd listen address' default=localhost:9124"`
}

func UpCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &UpConfig{
		MainConfig: mainCfg,
		LogdAddr:   "localhost:9123",
		DocdAddr:   "localhost:9124",
	}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Up, "up").
		WithSynopsis("up -data <dir>").
		WithDescription("start logd and docd servers").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return systemUp(cfg, cc, args)
		})
}

func systemUp(cfg *UpConfig, cc *cli.Context, args []string) error {
	_, err := cfg.Up.Parse(cc, args)
	if err != nil {
		return err
	}

	if cfg.DataDir == "" {
		return fmt.Errorf("-data is required")
	}

	// Start gops agent for debugging
	if err := agent.Listen(agent.Options{}); err != nil {
		fmt.Fprintf(cc.Out, "gops agent failed: %v\n", err)
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(cc.Out, "\nShutting down...\n")
		cancel()
	}()

	// Initialize logd storage
	s, err := storage.Open(cfg.DataDir, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create and start logd server
	logdSrv := logdserver.New(&logdserver.Spec{
		Storage: s,
	})

	if err := logdSrv.StartTCP(cfg.LogdAddr); err != nil {
		return fmt.Errorf("failed to start logd: %w", err)
	}
	fmt.Fprintf(cc.Out, "logd listening on %s\n", logdSrv.TCPAddr())
	defer logdSrv.StopTCP()

	// Create transaction pool (connects to logd with retry)
	txPool := txpool.New(&txpool.Config{
		LogdAddr: cfg.LogdAddr,
		PoolSize: 10,
	})
	defer txPool.Close()

	// Connect txpool in background (with retry)
	go func() {
		if err := txPool.Connect(ctx); err != nil {
			if ctx.Err() == nil {
				fmt.Fprintf(cc.Out, "txpool connect failed: %v\n", err)
			}
			return
		}
		// Prefetch TxIDs for common participant counts
		txPool.Prefetch(ctx, 1, 2, 3)
	}()

	// Create and start docd server
	docdSrv := docdserver.New(&docdserver.Spec{
		LogdAddr: cfg.LogdAddr,
	})

	if err := docdSrv.StartTCP(cfg.DocdAddr); err != nil {
		return fmt.Errorf("failed to start docd: %w", err)
	}
	fmt.Fprintf(cc.Out, "docd listening on %s\n", docdSrv.TCPAddr())
	defer docdSrv.StopTCP()

	fmt.Fprintf(cc.Out, "System up. Press Ctrl+C to stop.\n")

	// Wait for shutdown signal
	<-ctx.Done()

	return nil
}
