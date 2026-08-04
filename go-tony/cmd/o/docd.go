package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/gops/agent"
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/system/admin"
	"github.com/signadot/tony-format/go-tony/system/docd/server"
	"github.com/signadot/tony-format/go-tony/system/libctl"
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
			DocDServeCommand(cfg),
			DocDMetaCommand(cfg, "mounts", "list controller mounts on a running docd"),
			DocDMetaCommand(cfg, "schema", "show per-mount schema contributions on a running docd"))
}

type DocDMetaConfig struct {
	*DocDConfig
	Cmd      *cli.Command
	resource string
	Addr     string `cli:"name=addr desc='docd client-facing address' default=localhost:9124"`
}

// DocDMetaCommand builds a subcommand that reads one docd .meta resource (e.g.
// "mounts", "schema") from a running docd and renders it.
func DocDMetaCommand(docdCfg *DocDConfig, resource, desc string) *cli.Command {
	cfg := &DocDMetaConfig{DocDConfig: docdCfg, resource: resource, Addr: "localhost:9124"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Cmd, resource).
		WithSynopsis(resource + " [-addr <addr>]").
		WithDescription(desc).
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return docdMeta(cfg, cc, args)
		})
}

func docdMeta(cfg *DocDMetaConfig, cc *cli.Context, args []string) error {
	if _, err := cfg.Cmd.Parse(cc, args); err != nil {
		return err
	}

	// Query docd's .meta over the normal client protocol.
	session := libctl.NewLogdSession(&libctl.LogdSessionConfig{
		Addr:     cfg.Addr,
		ClientID: "o-docd-" + cfg.resource,
		// Quiet: this is a one-shot query, not a long-lived session.
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := session.Match(ctx, ".meta/"+cfg.resource)
	if err != nil {
		return fmt.Errorf("failed to query docd at %s: %w", cfg.Addr, err)
	}
	return encode.Encode(body, cc.Out, cfg.MainConfig.encOpts(cc.Out)...)
}

type DocDServeConfig struct {
	*DocDConfig
	Serve      *cli.Command
	ConfigFile string `cli:"name=config desc='configuration file (tony format)'"`
	Addr       string `cli:"name=addr desc='client-facing TCP listen address (logd session protocol)' default=localhost:9124"`
	MountAddr  string `cli:"name=mount-addr desc='controller-facing (MOUNT) TCP listen address' default=localhost:9125"`
	LogdAddr   string `cli:"name=logd desc='logd server address' default=localhost:9123"`
	AdminAddr  string `cli:"name=admin-addr desc='admin/pprof listen address, or off to disable' default=localhost:9224"`
}

func DocDServeCommand(docdCfg *DocDConfig) *cli.Command {
	cfg := &DocDServeConfig{DocDConfig: docdCfg, Addr: "localhost:9124", MountAddr: "localhost:9125", LogdAddr: "localhost:9123", AdminAddr: "localhost:9224"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve [-addr <addr>] [-mount-addr <addr>] [-logd <addr>] [-admin-addr <addr>]").
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

	// The admin listener comes up before anything that can wedge: it is the
	// channel that has to answer when the data path cannot.
	adminSrv := admin.New(&admin.Spec{
		Addr: cfg.AdminAddr,
		Name: "o docd serve",
		Log:  slog.Default(),
	})
	if err := adminSrv.Start(); err != nil {
		return err
	}
	defer adminSrv.Close()
	if a := adminSrv.Addr(); a != "" {
		fmt.Fprintf(cc.Out, "admin listening on %s (%s/debug/pprof/)\n", a, adminSrv.URL())
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
	adminSrv.SetAddrs(
		admin.Addr{Name: "docd-client", Addr: srv.ClientTCPAddr()},
		admin.Addr{Name: "docd-mount", Addr: srv.TCPAddr()},
		admin.Addr{Name: "logd-upstream", Addr: cfg.LogdAddr},
	)

	// Block forever
	select {}
}
