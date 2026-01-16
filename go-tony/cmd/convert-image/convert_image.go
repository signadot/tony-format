package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"

	imageref "github.com/novln/docker-parser"
)

var (
	registry  = ""
	repo      = "signadot"
	suffix    = ""
	tag       = "latest"
	noNewline = false
)

const (
	defaultRegistry = "docker.io"
)

func main() {
	flag.StringVar(&registry, "registry", registry, "docker registry")
	flag.StringVar(&repo, "repo", repo, "docker repository")
	flag.StringVar(&suffix, "suffix", suffix, "image suffix")
	flag.StringVar(&tag, "tag", tag, "tag")
	flag.BoolVar(&noNewline, "n", noNewline, "do not output trailing newline")
	flag.Parse()
	d, err := io.ReadAll(os.Stdin)
	if err != nil {
		slog.Error("error reading input", "error", err)
		os.Exit(1)
	}
	image := string(bytes.TrimSpace(d))
	fmt.Fprintf(os.Stderr, "read image %q\n", image)
	ref, err := imageref.Parse(image)
	if err != nil {
		slog.Error("error parsing image", "image", image, "error", err)
		os.Exit(1)
	}

	inReg := ref.Registry()
	if inReg == defaultRegistry {
		inReg = ""
	}
	if inReg != "" {
		if registry == "" {
			registry = inReg
		}
	}
	inRepo, inName := path.Split(ref.ShortName())
	if inRepo != "" {
		if repo == "" {
			repo = inRepo
		}
	}
	name := path.Join(repo, inName)
	if repo == "" {
		name = inName
	}

	inTag := ref.Tag()
	if tag == "" {
		tag = inTag
	}

	if registry != "" {
		registry += "/"
	}

	format := "%s%s%s:%s\n"
	if noNewline {
		format = "%s%s%s:%s"
	}
	_, err = fmt.Fprintf(os.Stdout, format, registry, name, suffix, tag)
	if err != nil {
		slog.Error("error writing output", "error", err)
		os.Exit(1)
	}
}
