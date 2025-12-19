package main

import (
	"context"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/commands"
)

func main() {
	cli.MainContext(context.Background(), commands.Root())
}
