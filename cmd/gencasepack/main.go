package main

import (
	"encoding/json"
	"os"

	"github.com/example/tga-evidence/internal/casepack"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(casepack.ExportPackForGenerator()); err != nil {
		panic(err)
	}
}
