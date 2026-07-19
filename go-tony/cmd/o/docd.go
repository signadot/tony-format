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
	Addr       string `cli:"name=addr desc='client-facing TCP listen address (logd session protocol)' default=localhost:9124"`
	MountAddr  string `cli:"name=mount-addr desc='controller-facing (MOUNT) TCP listen address' default=localhost:9125"`
	LogdAddr   string `cli:"name=logd desc='logd server address' default=localhost:9123"`
}

func DocDServeCommand(docdCfg *DocDConfig) *cli.Command {
	cfg := &DocDServeConfig{DocDConfig: docdCfg, Addr: "localhost:9124", MountAddr: "localhost:9125", LogdAddr: "localhost:9123"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve [-addr <addr>] [-mount-addr <addr>] [-logd <addr>]").
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

	// Start the client-facing listener (logd session protocol, proxied to logd).
	if err := srv.StartClientTCP(cfg.Addr); err != nil {
		return fmt.Errorf("failed to start client TCP listener: %w", err)
	}
	defer srv.StopClientTCP()

	// Start the controller-facing (MOUNT) listener.
	if err := srv.StartTCP(cfg.MountAddr); err != nil {
		return fmt.Errorf("failed to start mount TCP listener: %w", err)
	}
	defer srv.StopTCP()

	fmt.Fprintf(cc.Out, "docd listening: client=%s mount=%s (logd: %s)\n",
		srv.ClientTCPAddr(), srv.TCPAddr(), cfg.LogdAddr)

	// Block forever
	select {}
}
