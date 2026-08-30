package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
)

type SchemaConfig struct {
	*MainConfig
	Schema *cli.Command
}

type SchemaCheckConfig struct {
	*MainConfig
	Check *cli.Command
}

func SchemaCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &SchemaConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Schema, "schema").
		WithSynopsis("schema <subcommand>").
		WithDescription("schema commands for validating documents").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return groupRun(cfg.Schema, cfg.MainConfig, cc, args)
		}).
		WithSubs(
			SchemaCheckCommand(cfg.MainConfig))
}

func SchemaCheckCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &SchemaCheckConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Check, "check").
		WithSynopsis("check <schema-file> [doc-files...]").
		WithDescription(schemaCheckDesc).
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return schemaCheck(cfg, cc, args)
		})
}

const schemaCheckDesc = `validate documents against a schema

Every document of every file is checked, and every file is checked even after one
of them fails, so one run says how much is wrong rather than just that something
is.

With no files, stdin is read, as grep and cat do.

Exit codes are the tool's usual three: 0 when everything checked satisfies the
schema, 1 when something did not -- which is an answer about the document -- and 2
for a fault: a schema that cannot be read, a file that cannot be opened, or input
which is not object notation at all.`

func schemaCheck(cfg *SchemaCheckConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Check.Parse(cc, args)
	if err != nil {
		cfg.Check.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Check, cc, cfg.Help) {
		return nil
	}
	if err := cfg.oneFormat(cfg.Check, cc); err != nil {
		return err
	}
	if len(args) < 1 {
		return usageErr(cfg.Check, cc, "schema check requires at least 1 argument (schema file)")
	}

	schemaFile := args[0]
	docFiles := args[1:]

	// Parse the schema
	s, err := loadSchema(cfg, cc, schemaFile)
	if err != nil {
		return fault(cc, fmt.Errorf("failed to load schema %s: %w", schemaFile, err))
	}

	// If no doc files, read from stdin
	if len(docFiles) == 0 {
		return checkResult(cc, checkReader(cfg, cc, s, "-", cc.In))
	}

	// Every file is checked, and every failure reported: a checker which stops at
	// the first bad file has to be run once per file to find out how much is wrong.
	var failed error
	for _, docFile := range docFiles {
		if err := checkFile(cfg, cc, s, docFile); err != nil {
			fmt.Fprintln(cc.Err, err)
			if failed == nil || !isInvalid(err) {
				failed = err
			}
		}
	}
	if failed == nil {
		return nil
	}
	if isInvalid(failed) {
		return cli.ExitCodeErr(1)
	}
	return cli.ExitCodeErr(2)
}

// A document which does not satisfy the schema is an ANSWER about the document,
// the way `o match` finding nothing is, so it exits 1. Exit 2 is kept for a
// fault: a schema that cannot be read, a file that cannot be opened, input that
// is not object notation at all. The two are worth telling apart, since a script
// which cannot distinguish "this manifest is wrong" from "I could not find the
// schema" reports the second as the first.
type invalidError struct{ error }

func (e invalidError) Unwrap() error { return e.error }

func isInvalid(err error) bool {
	var iv invalidError
	return errors.As(err, &iv)
}

// checkResult turns one check's error into the exit code for it.
func checkResult(cc *cli.Context, err error) error {
	if err == nil {
		return nil
	}
	if isInvalid(err) {
		fmt.Fprintln(cc.Err, err)
		return cli.ExitCodeErr(1)
	}
	return fault(cc, err)
}

func loadSchema(cfg *SchemaCheckConfig, cc *cli.Context, file string) (*schema.Schema, error) {
	var r io.Reader
	if file == "-" {
		// cc.In, not os.Stdin: what a caller redirects is the context's.
		r = cc.In
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	node, err := parse.Parse(data, cfg.parseOpts()...)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Merge base definitions (primitive types like string, number, etc.)
	if err := schema.MergeBaseDefinitions(node); err != nil {
		return nil, fmt.Errorf("failed to merge base definitions: %w", err)
	}

	return schema.ParseSchema(node)
}

func checkFile(cfg *SchemaCheckConfig, cc *cli.Context, s *schema.Schema, file string) error {
	var r io.Reader
	if file == "-" {
		r = cc.In
	} else {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("error opening %s: %w", file, err)
		}
		defer f.Close()
		r = f
	}
	return checkReader(cfg, cc, s, file, r)
}

func checkReader(cfg *SchemaCheckConfig, cc *cli.Context, s *schema.Schema, name string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", name, err)
	}

	// Split on document separator for multi-doc files. splitDocs is the one place
	// that knows what separates them, on the way in and on the way out.
	docs := splitDocs(data)
	var bad []error
	for i, docData := range docs {
		node, err := parse.Parse(docData, cfg.parseOpts()...)
		if err != nil {
			// A parse failure is a fault and it is not local to the document: what
			// splitDocs took for one document may not have been one, so the rest of
			// this file is not worth reporting on.
			return fmt.Errorf("parse error in %s (doc %d): %w", name, i, err)
		}

		if err := s.Validate(node); err != nil {
			bad = append(bad, fmt.Errorf("validation failed for %s (doc %d): %w", name, i, err))
		}
	}
	if len(bad) != 0 {
		return invalidError{errors.Join(bad...)}
	}

	if name == "-" {
		fmt.Fprintf(cc.Out, "stdin: ok\n")
	} else {
		fmt.Fprintf(cc.Out, "%s: ok\n", name)
	}
	return nil
}
