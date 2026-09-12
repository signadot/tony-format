package main

import (
	"fmt"
	"log/slog"

	"github.com/google/gops/agent"
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/system/admin"
	"github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type LogDConfig struct {
	*MainConfig
	LogD *cli.Command
}

func LogDCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &LogDConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.LogD, "logd").
		WithSynopsis("logd <subcommand>").
		WithDescription("logd storage server commands").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return groupRun(cfg.LogD, cfg.MainConfig, cc, args)
		}).
		WithSubs(
			LogDServeCommand(cfg))
}

type LogDServeConfig struct {
	*LogDConfig
	Serve      *cli.Command
	DataDir    string `cli:"name=data desc='directory for logd data'"`
	ConfigFile string `cli:"name=config desc='configuration file (tony format)'"`
	Addr       string `cli:"name=addr desc='TCP listen address' default=localhost:9123"`
	AdminAddr  string `cli:"name=admin-addr desc='admin/pprof listen address, or off to disable' default=localhost:9223"`
}

func LogDServeCommand(logdCfg *LogDConfig) *cli.Command {
	cfg := &LogDServeConfig{LogDConfig: logdCfg, Addr: "localhost:9123", AdminAddr: "localhost:9223"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve -data <dir> [-addr <addr>] [-admin-addr <addr>]").
		WithDescription("run the logd storage server").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return logdServe(cfg, cc, args)
		})
}

func logdServe(cfg *LogDServeConfig, cc *cli.Context, args []string) error {
	_, err := cfg.Serve.Parse(cc, args)
	if err != nil {
		return err
	}
	if helpAsked(cfg.Serve, cc, cfg.Help) {
		return nil
	}

	// Start gops agent for debugging
	if err := agent.Listen(agent.Options{}); err != nil {
		fmt.Fprintf(cc.Out, "gops agent failed: %v\n", err)
	}

	// The admin listener comes up before anything that can wedge: it is the
	// channel that has to answer when the data path cannot.
	adminSrv := admin.New(&admin.Spec{
		Addr: cfg.AdminAddr,
		Name: "o logd serve",
		Log:  slog.Default(),
	})
	if err := adminSrv.Start(); err != nil {
		return err
	}
	defer adminSrv.Close()
	if a := adminSrv.Addr(); a != "" {
		fmt.Fprintf(cc.Out, "admin listening on %s (%s/debug/pprof/)\n", a, adminSrv.URL())
	}

	if cfg.DataDir == "" {
		return fmt.Errorf("-data is required")
	}

	// Load configuration
	var serverConfig *server.Config
	if cfg.ConfigFile != "" {
		serverConfig, err = server.LoadConfig(cfg.ConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Initialize storage
	s, err := storage.Open(cfg.DataDir, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// The admin listener came up before the store; now that there is one, let it
	// report what reads are doing. Whether a read at a path narrows or reads the
	// whole document cannot be told from outside (ap8ddvp2h12krd43gdn0).
	adminSrv.SetReport(func() map[string]any { return s.StatsReport() })

	// Create server
	srv := server.New(&server.Spec{
		Config:  serverConfig,
		Storage: s,
	})

	// Start TCP listener
	if err := srv.StartTCP(cfg.Addr); err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	fmt.Fprintf(cc.Out, "TCP session listener on %s\n", srv.TCPAddr())
	defer srv.StopTCP()
	adminSrv.SetAddrs(admin.Addr{Name: "logd", Addr: srv.TCPAddr()})

	// Block forever
	select {}
}
