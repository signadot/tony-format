package dirbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/format"
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
	for _, dirEnt := range dirEnts {
		if dirEnt.IsDir() {
			continue
		}
		fName := dirEnt.Name()
		for _, f := range format.AllFormats() {
			suffix := f.Suffix()
			if strings.HasSuffix(fName, suffix) {
				res = append(res, fName[:len(fName)-len(suffix)])
				break
			}
		}
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
	profIR, err := parse.Parse(dd)
	if err != nil {
		return err
	}
	patch := ir.Get(profIR, "env")
	if patch == nil {
		return fmt.Errorf("no env in profile at %s", profilePath)
	}
	tool := tony.DefaultTool()
	tool.Env = env
	runPatch, err := tool.Run(patch)
	if err != nil {
		return err
	}

	irEnv, err := eval.FromAny(d.Env)
	if err != nil {
		return err
	}
	resIR, err := tony.Patch(irEnv, runPatch)
	if err != nil {
		return err
	}
	envIR, err := eval.MapAnyToIR(env)
	if err != nil {
		return err
	}
	merged, err := tony.Patch(resIR, envIR)
	if err != nil {
		return err
	}
	reDir, err := OpenDir(d.Root, ir.ToMap(merged))
	if err != nil {
		return err
	}
	*d = *reDir
	return nil
}

func (d *Dir) profilePath(profile string) (string, error) {
	// try just the file path as-is
	st, err := os.Stat(profile)
	if err == nil {
		if !st.IsDir() {
			return profile, nil
		}
		return "", fmt.Errorf("profile path %s is a directory", profile)
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	// try each format suffix in the profiles directory
	for _, f := range format.AllFormats() {
		path := filepath.Join(d.Root, "profiles", profile+f.Suffix())
		st, err = os.Stat(path)
		if err == nil {
			if !st.IsDir() {
				return path, nil
			}
			return "", fmt.Errorf("profile path %s is a directory", path)
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("profile %q not found in profiles/", profile)
}
