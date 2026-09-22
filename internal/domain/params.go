package domain

import "fmt"

// ParamsFingerprint is the stable parameter fingerprint quoted on every
// derived value (algorithm version + parameter hash).
func ParamsFingerprint(p Params) string {
	return fmt.Sprintf("%s:%s", p.Algorithm, ShortID(Fingerprint(p), 12))
}

// DependsOn returns true if a layer change between two source sets affects
// a given dependency list — the basis for local invalidation.
func DependsOn(deps []Layer, changed map[Layer]bool) bool {
	for _, l := range deps {
		if changed[l] {
			return true
		}
	}
	return false
}

// ChangedLayers compares two source sets by layer+payload hash.
func ChangedLayers(a, b []SourceRef) map[Layer]bool {
	ha := map[Layer]string{}
	for _, s := range a {
		ha[s.Layer] = s.Hash
	}
	out := map[Layer]bool{}
	for _, s := range b {
		if ha[s.Layer] != s.Hash {
			out[s.Layer] = true
		}
	}
	for l := range ha {
		if _, ok := map[Layer]bool{}[l]; !ok {
			if _, found := findLayer(b, l); !found {
				out[l] = true
			}
		}
	}
	return out
}

func findLayer(ss []SourceRef, l Layer) (SourceRef, bool) {
	for _, s := range ss {
		if s.Layer == l {
			return s, true
		}
	}
	return SourceRef{}, false
}
