package main

import (
	"fmt"
	"net/http"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type LogDConfig struct {
	MainConfig *MainConfig
	LogD       *cli.Command
	DataDir    string `cli:"name=data desc='directory for logd data'"`
	Port       int    `cli:"name=port desc='HTTP server port default 9000'"`
}

func LogDCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &LogDConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.LogD, "logd").
		WithSynopsis("logd -data -port").
		WithDescription("run the logd backend server").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return logd(cfg, cc, args)
		})
}

func logd(cfg *LogDConfig, cc *cli.Context, args []string) error {
	_, err := cfg.LogD.Parse(cc, args)
	if err != nil {
		return err
	}

	if cfg.DataDir == "" {
		return fmt.Errorf("-data is required")
	}

	// Initialize storage
	s, err := storage.Open(cfg.DataDir, 022, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create server
	srv := server.New(s)

	// Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	fmt.Printf("Starting logd server on %s\n", addr)
	return http.ListenAndServe(addr, srv)
}
