package main

import (
	"log/slog"
	"os"
)

var (
	theLog = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			if a.Key == slog.LevelKey {
				if a.Value.String() == "INFO" {
					return slog.Attr{}
				}
			}
			return a
		},
	}))
)
