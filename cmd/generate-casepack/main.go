// Command generate-casepack regenerates the deterministic offline case pack.
package main

import (
	"log"

	"tga-review/internal/casepack"
)

func main() {
	if err := casepack.Generate("internal/casepack"); err != nil {
		log.Fatal(err)
	}
}
