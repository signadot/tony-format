package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/gomap/codegen"
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

	return cli.NewCommand("tony-codegen").
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
}

func run(cfg *Config, cc *cli.Context, args []string) error {
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

	// Separate structs by mode (schemadef= vs schema=)
	var schemadefStructs []*codegen.StructInfo
	var schemaStructs []*codegen.StructInfo

	for _, s := range allStructs {
		if s.StructSchema == nil {
			continue
		}
		if s.StructSchema.Mode == "schemadef" {
			schemadefStructs = append(schemadefStructs, s)
		} else if s.StructSchema.Mode == "schema" {
			schemaStructs = append(schemaStructs, s)
		}
	}

	// Track generated schemas (needed for loading schemas later)
	generatedSchemas := make(map[string]*codegen.GeneratedSchema)

	// Step 1: Build dependency graph for schemadef structs
	if len(schemadefStructs) > 0 {
		graph, err := codegen.BuildDependencyGraph(schemadefStructs)
		if err != nil {
			return fmt.Errorf("failed to build dependency graph: %w", err)
		}

		// Step 2: Detect circular dependencies
		cycles, err := codegen.DetectCycles(graph)
		if err != nil {
			return fmt.Errorf("failed to detect cycles: %w", err)
		}
		if len(cycles) > 0 {
			return fmt.Errorf("circular dependencies detected: %v", cycles)
		}

		// Step 3: Topological sort (get ordered list)
		orderedStructs, err := codegen.TopologicalSort(graph)
		if err != nil {
			return fmt.Errorf("failed to sort structs: %w", err)
		}

		// Step 4: Generate schemas (in dependency order)
		// Pass all structs to GenerateSchema so the struct map includes all possible references
		for _, structInfo := range orderedStructs {
			// Generate schema for this struct (pass all structs so references can be resolved)
			schemaNode, err := codegen.GenerateSchema(allStructs, structInfo)
			if err != nil {
				return fmt.Errorf("failed to generate schema for %q: %w", structInfo.Name, err)
			}

			// Determine output path for schema
			schemaName := structInfo.StructSchema.SchemaName
			schemaPath, err := determineSchemaPath(config, pkg, schemaName)
			if err != nil {
				return fmt.Errorf("failed to determine schema path for %q: %w", schemaName, err)
			}

			// Write schema file
			if err := codegen.WriteSchema(schemaNode, schemaPath); err != nil {
				return fmt.Errorf("failed to write schema file %q: %w", schemaPath, err)
			}

			// Store generated schema for later use
			generatedSchemas[schemaName] = &codegen.GeneratedSchema{
				Name:     schemaName,
				IRNode:   schemaNode,
				FilePath: schemaPath,
			}
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

	// Load newly generated schemas for schemadef= structs
	for _, structInfo := range schemadefStructs {
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
