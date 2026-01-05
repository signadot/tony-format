package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/ir"
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
	return cli.NewCommandAt(&cfg.Schema, "schema").
		WithSynopsis("schema <subcommand>").
		WithDescription("schema commands for validating documents").
		WithSubs(
			SchemaCheckCommand(cfg.MainConfig))
}

func SchemaCheckCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &SchemaCheckConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.Check, "check").
		WithSynopsis("check <schema-file> [doc-files...]").
		WithDescription("validate documents against a schema").
		WithRun(func(cc *cli.Context, args []string) error {
			return schemaCheck(cfg, cc, args)
		})
}

func schemaCheck(cfg *SchemaCheckConfig, cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: schema check requires at least 1 argument (schema file)", cli.ErrUsage)
	}

	schemaFile := args[0]
	docFiles := args[1:]

	// Parse the schema
	s, err := loadSchema(cfg, schemaFile)
	if err != nil {
		return fmt.Errorf("failed to load schema %s: %w", schemaFile, err)
	}

	// If no doc files, read from stdin
	if len(docFiles) == 0 {
		return checkReader(cfg, cc, s, "-", cc.In)
	}

	// Check each document file
	for _, docFile := range docFiles {
		if err := checkFile(cfg, cc, s, docFile); err != nil {
			return err
		}
	}

	return nil
}

func loadSchema(cfg *SchemaCheckConfig, file string) (*schema.Schema, error) {
	var r io.Reader
	if file == "-" {
		r = os.Stdin
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
	if err := mergeBaseDefinitions(node); err != nil {
		return nil, fmt.Errorf("failed to merge base definitions: %w", err)
	}

	return schema.ParseSchema(node)
}

// mergeBaseDefinitions merges base.tony definitions into the schema node.
// Base definitions are only added if they don't already exist in the schema.
// Excludes _ir and ir definitions which have circular references that cause
// infinite expansion during validation.
func mergeBaseDefinitions(node *ir.Node) error {
	baseDefs, err := schema.BaseDefinitions()
	if err != nil {
		return err
	}
	if baseDefs == nil {
		return nil
	}

	// Definitions to skip - they have circular references that cause infinite expansion
	skipDefs := map[string]bool{
		"_ir": true,
		"ir":  true,
	}

	// Get or create the define section
	defineNode := ir.Get(node, "define")
	if defineNode == nil {
		// Create define section
		defineNode = ir.FromMap(map[string]*ir.Node{})
		node.Fields = append(node.Fields, ir.FromString("define"))
		node.Values = append(node.Values, defineNode)
	}

	// Build a set of existing definition names
	existing := make(map[string]bool)
	for _, field := range defineNode.Fields {
		if field.Type == ir.StringType {
			existing[field.String] = true
		}
	}

	// Add base definitions that don't already exist (except skip list)
	for name, def := range baseDefs {
		if !existing[name] && !skipDefs[name] {
			defineNode.Fields = append(defineNode.Fields, ir.FromString(name))
			defineNode.Values = append(defineNode.Values, def)
		}
	}

	return nil
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

	// Split on document separator for multi-doc files
	docs := bytes.Split(data, []byte("\n---\n"))
	for i, docData := range docs {
		node, err := parse.Parse(docData, cfg.parseOpts()...)
		if err != nil {
			return fmt.Errorf("parse error in %s (doc %d): %w", name, i, err)
		}

		if err := s.Validate(node); err != nil {
			return fmt.Errorf("validation failed for %s (doc %d): %w", name, i, err)
		}
	}

	if name == "-" {
		fmt.Fprintf(cc.Out, "stdin: ok\n")
	} else {
		fmt.Fprintf(cc.Out, "%s: ok\n", name)
	}
	return nil
}
