# tony-codegen: map[string]StructType generates invalid Go code

## Bug

`tony-codegen` fails when a struct field uses a map type with a struct value, e.g. `map[string]RepoConfig`.

## Reproduction

Given this Go type:

```go
//tony:schemagen=repo-config
type RepoConfig struct {
    Status StatusConfig `tony:"field=status, optional"`
}

//tony:schemagen=gateway-config
type GatewayConfig struct {
    Repos map[string]RepoConfig `tony:"field=repos, optional"`
}
```

Running `tony-codegen` generates an `isZeroValue` helper with an incorrect type:

```go
func isZeroValue_GatewayConfig_Repos(v map[string]struct) bool {
    return len(v) == 0
}
```

The value type `RepoConfig` is emitted as `struct` (the Go keyword) instead of the qualified type name, causing a compilation error.

## Workaround

Remove the `tony:"..."` struct tag from the map field so codegen skips it. The reflection-based `gomap.FromTonyIR` still handles the map correctly at runtime.