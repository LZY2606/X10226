package cases

import (
	"os"
	"path/filepath"

	"tga-review/internal/ingest"
)

// LoadDir reads all stratified files of a packed variant directory.
func LoadDir(dir string) ([]ingest.File, error) {
	names := []string{
		"raw_points.csv", "balance_drift.csv", "furnace_program.csv",
		"gas_valve.csv", "blank_curve.csv", "initial_mass.csv", "params.json",
	}
	var files []ingest.File
	for _, n := range names {
		p := filepath.Join(dir, n)
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files = append(files, ingest.File{Name: n, Content: b})
	}
	return files, nil
}

// VariantDir returns the standard pack location of a variant.
func VariantDir(root, variant string) string { return filepath.Join(root, "cases", variant) }
