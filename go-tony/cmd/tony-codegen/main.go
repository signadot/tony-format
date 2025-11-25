package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/gomap/codegen"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func main() {
	cli.MainContext(context.Background(), MainCommand())
}

func MainCommand() *cli.Command {
	cfg := &Config{}
	sOpts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}

	return cli.NewCommandAt(&cfg.Command, "tony-codegen").
		WithSynopsis("tony-codegen [opts]").
		WithDescription("Generate .tony schema files and Go code (ToTony/FromTony methods) from structs with schema tags.").
		WithOpts(sOpts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return run(cfg, cc, args)
		})
}

type Config struct {
	OutputFile     string `cli:"name=o desc='output file for generated Go code (default: <package>_gen.go)'"`
	SchemaDir      string `cli:"name=schema-dir desc='directory for generated .tony schema files, preserves package structure'"`
	SchemaDirFlat  string `cli:"name=schema-dir-flat desc='directory for generated .tony schema files, flat structure (all schemas in one directory)'"`
	Dir            string `cli:"name=dir desc='directory to scan for Go files (default: current directory)'"`
	Recursive      bool   `cli:"name=recursive desc='scan subdirectories recursively'"`
	SchemaRegistry string `cli:"name=schema-registry desc='path to schema registry for cross-package references (optional)'"`

	Command *cli.Command
}

func run(cfg *Config, cc *cli.Context, args []string) error {
	args, err := cfg.Command.Parse(cc, args)
	if err != nil {
		return err
	}

	// Set default directory to current directory if not specified
	dir := cfg.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Validate that only one of schema-dir or schema-dir-flat is set
	if cfg.SchemaDir != "" && cfg.SchemaDirFlat != "" {
		return fmt.Errorf("%w: cannot specify both -schema-dir and -schema-dir-flat", cli.ErrUsage)
	}

	// Discover packages
	packages, err := codegen.DiscoverPackages(dir, cfg.Recursive)
	if err != nil {
		return fmt.Errorf("failed to discover packages: %w", err)
	}

	if len(packages) == 0 {
		return fmt.Errorf("no Go packages found in %q", dir)
	}

	// Process each package
	for _, pkg := range packages {
		fmt.Printf("Processing package: %s\n", pkg.Name)
		if err := processPackage(cfg, pkg); err != nil {
			return fmt.Errorf("failed to process package %q: %w", pkg.Path, err)
		}
	}

	return nil
}

func processPackage(cfg *Config, pkg *codegen.PackageInfo) error {
	// Build codegen config
	config := &codegen.CodegenConfig{
		OutputFile:     cfg.OutputFile,
		SchemaDir:      cfg.SchemaDir,
		SchemaDirFlat:  cfg.SchemaDirFlat,
		Dir:            pkg.Dir,
		Recursive:      false, // Already handled at package discovery level
		SchemaRegistry: cfg.SchemaRegistry,
		Package:        pkg,
	}

	// Set default output file if not specified
	if config.OutputFile == "" {
		config.OutputFile = filepath.Join(pkg.Dir, pkg.Name+"_gen.go")
	}

	// Parse all structs from all files in the package
	var allStructs []*codegen.StructInfo
	for _, filePath := range pkg.Files {
		file, _, err := codegen.ParseFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to parse file %q: %w", filePath, err)
		}

		structs, err := codegen.ExtractStructs(file, filePath)
		if err != nil {
			return fmt.Errorf("failed to extract structs from %q: %w", filePath, err)
		}

		// Set package name for each struct
		for _, s := range structs {
			s.Package = pkg.Name
		}

		allStructs = append(allStructs, structs...)
	}

	if len(allStructs) == 0 {
		// No structs with schema tags, skip this package
		return nil
	}

	// Resolve field types (convert AST types to reflect.Type)
	if err := codegen.ResolveFieldTypes(allStructs, pkg.Dir, pkg.Name); err != nil {
		return fmt.Errorf("failed to resolve field types: %w", err)
	}

	// Flatten embedded fields
	if err := codegen.FlattenEmbeddedFields(allStructs); err != nil {
		return fmt.Errorf("failed to flatten embedded fields: %w", err)
	}

	// Separate structs by mode (schemagen= vs schema=)
	var schemagenStructs []*codegen.StructInfo
	var schemaStructs []*codegen.StructInfo

	for _, s := range allStructs {
		if s.StructSchema == nil {
			continue
		}
		if s.StructSchema.Mode == "schemagen" {
			schemagenStructs = append(schemagenStructs, s)
		} else if s.StructSchema.Mode == "schema" {
			schemaStructs = append(schemaStructs, s)
		}
	}

	// Track generated schemas (needed for loading schemas later)
	generatedSchemas := make(map[string]*codegen.GeneratedSchema)

	// Step 1: Sort structs (Topological or Alphabetical fallback)
	var orderedStructs []*codegen.StructInfo
	if len(schemagenStructs) > 0 {
		// Build dependency graph
		graph, err := codegen.BuildDependencyGraph(schemagenStructs)
		if err != nil {
			fmt.Printf("Warning: Failed to build dependency graph: %v. Falling back to alphabetical sort.\n", err)
			orderedStructs = make([]*codegen.StructInfo, len(schemagenStructs))
			copy(orderedStructs, schemagenStructs)
			sort.Slice(orderedStructs, func(i, j int) bool {
				return orderedStructs[i].StructSchema.SchemaName < orderedStructs[j].StructSchema.SchemaName
			})
		} else {
			// Try topological sort
			sortedStructs, err := codegen.TopologicalSort(graph)
			if err != nil {
				// Cycles detected or other error
				fmt.Printf("Warning: Dependency cycles detected or sort failed: %v. Falling back to alphabetical sort.\n", err)
				orderedStructs = make([]*codegen.StructInfo, len(schemagenStructs))
				copy(orderedStructs, schemagenStructs)
				sort.Slice(orderedStructs, func(i, j int) bool {
					return orderedStructs[i].StructSchema.SchemaName < orderedStructs[j].StructSchema.SchemaName
				})
			} else {
				orderedStructs = sortedStructs
			}
		}
	}

	// Step 4: Generate schemas (in dependency order)
	// Collect all schemas to write to a single file
	var schemaNodes []*ir.Node
	loader := codegen.NewPackageLoader() // Shared loader for all schemas

	for _, structInfo := range orderedStructs {
		// Generate schema for this struct (pass all structs so references can be resolved)
		schemaNode, err := codegen.GenerateSchema(allStructs, structInfo, loader)
		if err != nil {
			return fmt.Errorf("failed to generate schema for %q: %w", structInfo.Name, err)
		}

		// Add to collection
		schemaNodes = append(schemaNodes, schemaNode)

		// Store generated schema for later use
		schemaName := structInfo.StructSchema.SchemaName
		generatedSchemas[schemaName] = &codegen.GeneratedSchema{
			Name:   schemaName,
			IRNode: schemaNode,
		}
	}

	// Write all schemas to a single file
	if len(schemaNodes) > 0 {
		// Determine output path - use first struct's package directory
		schemaPath := filepath.Join(pkg.Dir, "schema_gen.tony")
		if config.SchemaDir != "" {
			// If schema-dir is specified, use it
			schemaPath = filepath.Join(config.SchemaDir, filepath.Base(pkg.Dir), "schema_gen.tony")
		} else if config.SchemaDirFlat != "" {
			// If schema-dir-flat is specified, use it
			schemaPath = filepath.Join(config.SchemaDirFlat, "schema_gen.tony")
		}

		if err := codegen.WriteSchemasToSingleFile(schemaNodes, schemaPath); err != nil {
			return fmt.Errorf("failed to write schemas file %q: %w", schemaPath, err)
		}

		// Update all generated schemas with the same file path
		for schemaName := range generatedSchemas {
			generatedSchemas[schemaName].FilePath = schemaPath
		}
	}

	// Step 5: Load schemas (for schema= structs and newly generated ones)
	schemaCache := codegen.NewSchemaCache()
	schemas := make(map[string]*schema.Schema)

	// Load schemas for schema= structs
	for _, structInfo := range schemaStructs {
		schemaName := structInfo.StructSchema.SchemaName
		if _, ok := schemas[schemaName]; ok {
			continue // Already loaded
		}

		loaded, err := codegen.LoadSchema(schemaName, pkg.Dir, config, schemaCache, generatedSchemas)
		if err != nil {
			return fmt.Errorf("failed to load schema %q for struct %q: %w", schemaName, structInfo.Name, err)
		}
		schemas[schemaName] = loaded
	}

	// Load newly generated schemas for schemagen= structs
	for _, structInfo := range schemagenStructs {
		schemaName := structInfo.StructSchema.SchemaName
		if _, ok := schemas[schemaName]; ok {
			continue // Already loaded
		}

		// Load the schema we just generated
		loaded, err := codegen.LoadSchema(schemaName, pkg.Dir, config, schemaCache, generatedSchemas)
		if err != nil {
			return fmt.Errorf("failed to load generated schema %q: %w", schemaName, err)
		}
		schemas[schemaName] = loaded
	}

	// Step 6: Generate code (for all structs with schema tags)
	if len(allStructs) > 0 {
		// Generate code
		code, err := codegen.GenerateCode(allStructs, schemas, config)
		if err != nil {
			return fmt.Errorf("failed to generate code: %w", err)
		}

		// Write generated code
		if err := os.WriteFile(config.OutputFile, []byte(code), 0644); err != nil {
			return fmt.Errorf("failed to write output file %q: %w", config.OutputFile, err)
		}
	}

	return nil
}

func determineSchemaPath(config *codegen.CodegenConfig, pkg *codegen.PackageInfo, schemaName string) (string, error) {
	var schemaPath string

	if config.SchemaDirFlat != "" {
		// Flat structure: all schemas in one directory
		schemaPath = filepath.Join(config.SchemaDirFlat, schemaName+".tony")
	} else if config.SchemaDir != "" {
		// Preserve package structure
		// Get relative path from module root to package
		// For now, use package name as subdirectory
		schemaPath = filepath.Join(config.SchemaDir, pkg.Name, schemaName+".tony")
	} else {
		// Default: same directory as source files
		schemaPath = filepath.Join(pkg.Dir, schemaName+".tony")
	}

	// Ensure directory exists
	dir := filepath.Dir(schemaPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create schema directory %q: %w", dir, err)
	}

	return schemaPath, nil
}
