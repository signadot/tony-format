package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/gops/agent"
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/system/admin"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type UpConfig struct {
	*MainConfig
	Up            *cli.Command
	DataDir       string `cli:"name=data desc='data directory for logd storage'"`
	ConfigFile    string `cli:"name=config desc='logd configuration file (tony format), as for o system logd serve'"`
	LogdAddr      string `cli:"name=logd-addr desc='logd listen address' default=localhost:9123"`
	DocdAddr      string `cli:"name=docd-addr desc='docd client-facing listen address' default=localhost:9124"`
	DocdMountAddr string `cli:"name=docd-mount-addr desc='docd controller-facing (MOUNT) listen address' default=localhost:9125"`
	AdminAddr     string `cli:"name=admin-addr desc='admin/pprof listen address, or off to disable' default=localhost:9223"`
}

func UpCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &UpConfig{
		MainConfig:    mainCfg,
		LogdAddr:      "localhost:9123",
		DocdAddr:      "localhost:9124",
		DocdMountAddr: "localhost:9125",
		AdminAddr:     "localhost:9223",
	}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Up, "up").
		WithSynopsis("up -data <dir> [-config <file>]").
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
	if helpAsked(cfg.Up, cc, cfg.Help) {
		return nil
	}

	if cfg.DataDir == "" {
		return fmt.Errorf("-data is required")
	}

	// The same configuration file `o system logd serve` takes. Without this the
	// system stood up the documented way could not be configured at all — including
	// its snapshot policy, which is what bounds the cost of every read (issue
	// ps8kfs9dh12kr777fnn0). A nil config still gets logd's defaults, so the flag
	// overrides a policy rather than supplying the only one.
	var logdConfig *logdserver.Config
	if cfg.ConfigFile != "" {
		logdConfig, err = logdserver.LoadConfig(cfg.ConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load config %s: %w", cfg.ConfigFile, err)
		}
	}

	// Start gops agent for debugging
	if err := agent.Listen(agent.Options{}); err != nil {
		fmt.Fprintf(cc.Out, "gops agent failed: %v\n", err)
	}

	// The admin listener comes up before anything that can wedge: it is the
	// channel that has to answer when the data path cannot.
	adminSrv := admin.New(&admin.Spec{
		Addr: cfg.AdminAddr,
		Name: "o sys up",
		Log:  slog.Default(),
	})
	if err := adminSrv.Start(); err != nil {
		return err
	}
	defer adminSrv.Close()
	if a := adminSrv.Addr(); a != "" {
		fmt.Fprintf(cc.Out, "admin listening on %s (%s/debug/pprof/)\n", a, adminSrv.URL())
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
		Config:  logdConfig,
	})

	if err := logdSrv.StartTCP(cfg.LogdAddr); err != nil {
		return fmt.Errorf("failed to start logd: %w", err)
	}
	fmt.Fprintf(cc.Out, "logd listening on %s\n", logdSrv.TCPAddr())
	defer logdSrv.StopTCP()

	// docd owns and warms its own transaction-id pool (see StartClientTCP).

	// Create and start docd server: a client-facing listener (logd session
	// protocol, proxied/routed to logd and controllers) and a controller-facing
	// MOUNT listener.
	docdSrv := docdserver.New(&docdserver.Spec{
		LogdAddr: cfg.LogdAddr,
	})

	if err := docdSrv.StartClientTCP(cfg.DocdAddr); err != nil {
		return fmt.Errorf("failed to start docd client listener: %w", err)
	}
	defer docdSrv.StopClientTCP()

	if err := docdSrv.StartTCP(cfg.DocdMountAddr); err != nil {
		return fmt.Errorf("failed to start docd mount listener: %w", err)
	}
	defer docdSrv.StopTCP()

	fmt.Fprintf(cc.Out, "docd listening: client=%s mount=%s\n",
		docdSrv.ClientTCPAddr(), docdSrv.TCPAddr())

	adminSrv.SetAddrs(
		admin.Addr{Name: "logd", Addr: logdSrv.TCPAddr()},
		admin.Addr{Name: "docd-client", Addr: docdSrv.ClientTCPAddr()},
		admin.Addr{Name: "docd-mount", Addr: docdSrv.TCPAddr()},
	)

	fmt.Fprintf(cc.Out, "System up. Press Ctrl+C to stop.\n")

	// Wait for shutdown signal
	<-ctx.Done()

	return nil
}
