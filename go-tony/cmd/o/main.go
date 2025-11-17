package main

import (
	"context"

	"github.com/scott-cotton/cli"
	_ "github.com/signadot/tony-format/go-tony/eval"
	_ "github.com/signadot/tony-format/go-tony/mergeop"
)

func main() {
	cli.MainContext(context.Background(), MainCommand())
}
