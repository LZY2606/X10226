package casepack

import (
	"encoding/json"
	"os"
	"path/filepath"

	"tga-review/internal/synth"
)

// genOpts maps a scenario directory name to its synthetic features.
func genOpts(scenario string) synth.Options {
	base := func() synth.Options {
		return synth.Options{Step: 15, InitMass: 10}
	}
	o := base()
	o.Name = scenario
	switch scenario {
	case "baseline", "gas_delay", "drift", "narrative":
		// plain baseline; drift scenario changes drift in a second import
	case "overlap":
		o.Overlap = true
	case "nonmonotone":
		o.Noise = true
	case "gap":
		o.Gap = true
	case "halffail":
		o.BadRow = true
	case "initrev":
		o.InitMass = 10.15
	}
	return o
}

// Generate writes deterministic scenario materials into root/files and the
// manifest. Used by `go generate`; the outputs are committed to the repo so
// the offline pack is self-contained.
func Generate(root string) error {
	dirs := map[string]synth.Options{}
	for _, m := range scenarioList {
		for _, f := range m.Files {
			dir := filepath.Dir(f)
			if _, ok := dirs[dir]; !ok && dir != "." {
				dirs[dir] = genOpts(dir)
			}
		}
	}
	for dir, opt := range dirs {
		files := synth.Files(opt)
		if err := os.MkdirAll(filepath.Join(root, FilesRoot, dir), 0o755); err != nil {
			return err
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(root, FilesRoot, dir, name), []byte(content), 0o644); err != nil {
				return err
			}
		}
	}
	// initrev_base points at the baseline init mass but scenario uses revised
	if err := os.MkdirAll(filepath.Join(root, FilesRoot, "initrev_base"), 0o755); err != nil {
		return err
	}
	baseFiles := synth.Files(genOpts("baseline"))
	for name, content := range baseFiles {
		if err := os.WriteFile(filepath.Join(root, FilesRoot, "initrev_base", name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	// narrative baseline alias
	if err := os.MkdirAll(filepath.Join(root, FilesRoot, "narrative"), 0o755); err != nil {
		return err
	}
	for name, content := range synth.Files(genOpts("narrative")) {
		if err := os.WriteFile(filepath.Join(root, FilesRoot, "narrative", name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	// shifted drift variants for the local-dependency-change scenarios
	shift := synth.Files(synth.Options{Step: 15, InitMass: 10, DriftShift: true})
	for _, dir := range []string{"narrative", "drift"} {
		if err := os.WriteFile(filepath.Join(root, FilesRoot, dir, "drift_shifted.csv"),
			[]byte(shift["drift.csv"]), 0o644); err != nil {
			return err
		}
	}
	mb, err := json.MarshalIndent(scenarioList, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, FilesRoot, "manifest.json"), mb, 0o644)
}
