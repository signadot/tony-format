package mapstruct

// Regression fixture for issue cc5rbhv8h12k: a field whose type is an UNNAMED map
// literal with a generated-struct value (map[string]RepoConfig) must be inlined
// and dispatched per value — codegen used to emit s.Repos.ToTonyIR() (a method the
// map has no) because the value type carried a codec.

//tony:schemagen=mapstruct-status,notag
type StatusConfig struct {
	OK bool `tony:"field=ok,optional"`
}

//tony:schemagen=mapstruct-repo,notag
type RepoConfig struct {
	Branch string       `tony:"field=branch,optional"`
	Status StatusConfig `tony:"field=status,optional"`
}

//tony:schemagen=mapstruct-gateway,notag
type GatewayConfig struct {
	Repos map[string]RepoConfig `tony:"field=repos,optional"`
}
