package dirbuild

import (
	"fmt"
	"os"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/eval"
	"github.com/tony-format/tony/parse"
)

const (
	EnvEnv = "YTOOL_ENV"
)

func LoadEnv() (map[string]any, error) {
	envEnv := os.Getenv(EnvEnv)
	if envEnv == "" {
		return nil, nil
	}
	yEnv, err := parse.Parse([]byte(envEnv))
	if err != nil {
		return nil, fmt.Errorf("error decoding env $%s: %w", EnvEnv, err)
	}
	envAny := eval.ToJSONAny(yEnv)
	theEnvEnv, ok := envAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("error decoding env $%s: wrong type %T", EnvEnv, envAny)
	}
	if debug.LoadEnv() {
		debug.Logf("\nloaded env from env: %s\n", debug.JSON(theEnvEnv))
	}
	return theEnvEnv, nil
}
