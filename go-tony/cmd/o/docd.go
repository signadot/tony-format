package main

import (
	"fmt"

	"github.com/google/gops/agent"
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/system/docd/server"
)

type DocDConfig struct {
	*MainConfig
	DocD *cli.Command
}

func DocDCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &DocDConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.DocD, "docd").
		WithSynopsis("docd <subcommand>").
		WithDescription("docd document server commands").
		WithSubs(
			DocDServeCommand(cfg))
}

type DocDServeConfig struct {
	*DocDConfig
	Serve      *cli.Command
	ConfigFile string `cli:"name=config desc='configuration file (tony format)'"`
	Addr       string `cli:"name=addr desc='TCP listen address' default=localhost:9124"`
	LogdAddr   string `cli:"name=logd desc='logd server address' default=localhost:9123"`
}

func DocDServeCommand(docdCfg *DocDConfig) *cli.Command {
	cfg := &DocDServeConfig{DocDConfig: docdCfg, Addr: "localhost:9124", LogdAddr: "localhost:9123"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve [-addr <addr>] [-logd <addr>]").
		WithDescription("run the docd document server").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return docdServe(cfg, cc, args)
		})
}

func docdServe(cfg *DocDServeConfig, cc *cli.Context, args []string) error {
	_, err := cfg.Serve.Parse(cc, args)
	if err != nil {
		return err
	}

	// Start gops agent for debugging
	if err := agent.Listen(agent.Options{}); err != nil {
		fmt.Fprintf(cc.Out, "gops agent failed: %v\n", err)
	}

	// Load configuration
	var serverConfig *server.Config
	if cfg.ConfigFile != "" {
		serverConfig, err = server.LoadConfig(cfg.ConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Create server
	srv := server.New(&server.Spec{
		Config:   serverConfig,
		LogdAddr: cfg.LogdAddr,
	})

	// Start TCP listener
	if err := srv.StartTCP(cfg.Addr); err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	fmt.Fprintf(cc.Out, "docd listening on %s (logd: %s)\n", srv.TCPAddr(), cfg.LogdAddr)
	defer srv.StopTCP()

	// Block forever
	select {}
}
