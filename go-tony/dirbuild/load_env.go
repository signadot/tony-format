package dirbuild

import (
	"fmt"
	"os"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/parse"
)

// EnvEnv is the name of the environment variable that can contain
// additional environment configuration in tony/YAML/JSON format.
const EnvEnv = "TONY_DIRBUILD_ENV"

// LoadEnv reads and parses environment configuration from the TONY_DIRBUILD_ENV
// environment variable. Returns nil if the variable is not set. The variable's
// value should be a tony/YAML/JSON object that will be parsed and returned as
// a map for use with [OpenDir] or profile loading.
func LoadEnv() (map[string]any, error) {
	envEnv := os.Getenv(EnvEnv)
	if envEnv == "" {
		return nil, nil
	}
	node, err := parse.Parse([]byte(envEnv))
	if err != nil {
		return nil, fmt.Errorf("error decoding env $%s: %w", EnvEnv, err)
	}
	envAny := eval.ToAny(node)
	theEnvEnv, ok := envAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("error decoding env $%s: wrong type %T", EnvEnv, envAny)
	}
	if debug.LoadEnv() {
		debug.Logf("\nloaded env from env: %s\n", debug.JSON(theEnvEnv))
	}
	return theEnvEnv, nil
}
