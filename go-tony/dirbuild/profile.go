package dirbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func (d *Dir) Profiles() ([]string, error) {
	dirEnts, err := os.ReadDir(filepath.Join(d.Root, "profiles"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	res := []string{}
	suffix := d.profileSuffix()
	for _, dirEnt := range dirEnts {
		if dirEnt.IsDir() {
			continue
		}
		fName := dirEnt.Name()
		if !strings.HasSuffix(fName, suffix) {
			continue
		}
		res = append(res, fName[:len(fName)-len(suffix)])
	}
	return res, nil
}

func (d *Dir) LoadProfile(profile string, env map[string]any) error {
	if debug.LoadEnv() {
		debug.Logf("LoadProfile with env\n%s", debug.JSON(env))
	}
	profilePath, err := d.profilePath(profile)
	dd, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	yProfile, err := parse.Parse(dd)
	if err != nil {
		return err
	}
	patch := ir.Get(yProfile, "env")
	if patch == nil {
		return fmt.Errorf("no env in profile at %s", profilePath)
	}
	tool := tony.DefaultTool()
	tool.Env = env
	runPatch, err := tool.Run(patch)
	if err != nil {
		return err
	}

	yEnv, err := eval.FromJSONAny(d.Env)
	if err != nil {
		return err
	}
	yRes, err := tony.Patch(yEnv, runPatch)
	if err != nil {
		return err
	}
	yAny := eval.ToJSONAny(yRes)

	res, ok := yAny.(map[string]any)
	if !ok {
		return fmt.Errorf("wrong type for patched env %T", res)
	}
	merged, err := mergeEnv(res, env)
	if err != nil {
		return err
	}
	reDir, err := OpenDir(d.Root, merged)
	if err != nil {
		return err
	}
	*d = *reDir
	return nil
}

func (d *Dir) profilePath(profile string) (string, error) {
	// try just the file
	st, err := os.Stat(profile)
	if err == nil {
		if !st.IsDir() {
			return profile, nil
		}
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	path := filepath.Join(d.Root, "profiles", profile+d.profileSuffix())
	st, err = os.Stat(path)
	if err == nil {
		if !st.IsDir() {
			return path, nil
		}
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return "", fmt.Errorf("profilePath %s is a directory", path)
}

func (d *Dir) profileSuffix() string {
	return ".yaml"
}
