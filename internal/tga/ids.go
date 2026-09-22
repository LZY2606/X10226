package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Stable identifiers are content derived so that export -> wipe -> import
// reproduces every value identity, ordering and provenance link.

func shortHash(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hash16 returns a 16 hex char digest.
func hash16(parts ...string) string { return shortHash(parts...)[:16] }

// hash12 returns a 12 hex char digest used for display value ids.
func hash12(parts ...string) string { return shortHash(parts...)[:12] }

// canonicalJSON renders a value deterministically (map keys sorted).
func canonicalJSON(v any) string {
	b, err := json.Marshal(sortKeys(v))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortKeys(t[k])
		}
		return out
	case []any:
		for i := range t {
			t[i] = sortKeys(t[i])
		}
		return t
	default:
		return v
	}
}

// idEvent is a function of project, sequence, previous event and payload:
// replaying the same log regenerates identical ids.
func idEvent(projectID string, seq int64, prevID string, payloadJSON, atRFC3339 string) string {
	return "ev_" + hash16(projectID, i64(seq), prevID, atRFC3339, payloadJSON)
}

func idJob(eventID, label string) string { return "job_" + hash12(eventID, label) }
func idRecord(projectID, jobID, layer string, row int, rawLine string) string {
	return "rec_" + hash12(projectID, jobID, layer, i64(int64(row)), rawLine)
}
func idSource(projectID, layer, parentID, jobID, content string, seq int64) string {
	return "src_" + layer + "_" + hash12(projectID, layer, parentID, jobID, content, i64(seq))
}
func idParams(projectID, parent string, seq int64, body string) string {
	return "par_" + hash12(projectID, parent, i64(seq), body)
}
func idArtifact(kind string, material string) string {
	return "art_" + kind + "_" + hash12(material)
}
func idDecision(eventID, target, action string) string {
	return "dec_" + hash12(eventID, target, action)
}

// VID mints the stable identity of a single displayed value inside an
// artifact. path is a dotted coordinate inside the artifact payload.
func VID(artifactID, path string) string {
	return "val:" + artifactID + ":" + path
}
