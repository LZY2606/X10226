// Command gen-case regenerates the bundled offline TGA case pack.
// It is deterministic (fixed seed) and writes to testdata/cases.
package main

import (
	"flag"
	"log"

	"tga-review/internal/cases"
)

func main() {
	dir := flag.String("out", "testdata", "output directory (a 'cases' folder is created inside)")
	flag.Parse()
	if err := cases.WritePack(*dir); err != nil {
		log.Fatal(err)
	}
	log.Printf("offline TGA case pack written to %s/cases", *dir)
}
