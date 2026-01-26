// Package dirbuild provides a directory-based build system for processing
// and transforming tony/YAML/JSON documents.
//
// A build directory contains a build.{tony,yaml,json} file that defines:
//   - Sources: where to fetch input documents (directories, URLs, or commands)
//   - Patches: transformations to apply to matched documents
//   - Environment: variables available during evaluation
//
// # Basic Usage
//
// Open a build directory and run the build pipeline:
//
//	dir, err := dirbuild.OpenDir("/path/to/build", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	docs, err := dir.Run(os.Stdout)
//
// # Build File Structure
//
// A build file specifies sources, patches, and optional configuration:
//
//	build:
//	  suffix: .yaml           # output file suffix (optional)
//	  destDir: ./output       # output directory (optional, stdout if omitted)
//	  env:
//	    version: "1.0.0"
//	  sources:
//	    - dir: ./manifests    # read files from directory
//	    - url: https://...    # fetch from URL
//	    - exec: "cmd args"    # execute command
//	  patches:
//	    - match: {kind: Deployment}
//	      patch: {spec: {replicas: 3}}
//
// # Sources
//
// Sources define where input documents come from. Each source can specify:
//   - dir: path to a directory (walks recursively, reads .tony/.yaml/.json files)
//   - url: HTTP(S) URL to fetch
//   - exec: command to execute (stdout is parsed as documents)
//   - format: explicit format (tony, yaml, json) - auto-detected if omitted
//   - if: conditional expression to enable/disable the source
//
// # Patches
//
// Patches transform documents that match a pattern. Each patch specifies:
//   - match: pattern to match against documents
//   - patch: transformation to apply
//   - file: load patches from external file
//   - if: conditional expression to enable/disable the patch
//
// # Profiles
//
// Build directories can have a profiles/ subdirectory containing environment
// overrides. Load a profile with [Dir.LoadProfile]:
//
//	dir.LoadProfile("production", nil)
//
// # Environment Variables
//
// The [LoadEnv] function reads configuration from the TONY_DIRBUILD_ENV
// environment variable, allowing external configuration injection.
package dirbuild
