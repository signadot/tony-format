// Package dirbuild provides a directory-based build system for processing
// and transforming tony/YAML/JSON documents.
//
// A build directory contains a build.{tony,yaml,json} file that defines:
//   - Sources: where to fetch input documents (directories, URLs, or commands)
//   - Patches: transformations to apply to matched documents
//   - Environment: variables available during evaluation
//   - Output: where the resulting documents are written
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
//	  output:                 # optional
//	    destDir: ./output     # a file per document here; Run's writer if omitted
//	    suffix: .yaml         # file suffix; the output format's if omitted
//	    k8s:
//	      filenames: kind-name
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
// A build without output: takes destDir: and suffix: from build: itself.
//
// # Sources
//
// Sources define where input documents come from. Each source can specify:
//   - dir: path to a directory (walks recursively, reads .tony/.yaml/.json files,
//     skipping the glob patterns a .buildignore.tony lists)
//   - url: HTTP(S) URL to fetch
//   - exec: command to execute, split on whitespace and run without a shell
//     (stdout is parsed as documents)
//   - format: explicit format (tony, yaml, json); if omitted, a file's or URL's
//     extension decides, and tony otherwise
//   - if: conditional expression to enable/disable the source
//
// The dir:, url: and exec: strings are expanded against the environment with package
// eval, so $[version] is the variable's value.
//
// # Patches
//
// Patches transform documents that match a pattern. Each patch specifies:
//   - match: pattern to match against documents
//   - patch: transformation to apply
//   - file: load patches from external file
//   - if: conditional expression to enable/disable the patch
//
// match: and patch: are expanded against the environment the same way when the build
// directory is opened, and the patches apply in order, each to the document the one
// before it left.
//
// # Output
//
// Without output.destDir the documents are written to the writer [Dir.Run] is given,
// separated by ---. With it, each is written to a file of its own in that directory.
// A file is named by the document's !filename(name) tag, which is not written, and
// otherwise from its contents; output.k8s.filenames names a Kubernetes object by a
// dash-separated pattern of the tokens name, kind and namespace. A name used twice
// has -1, -2, ... appended.
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
