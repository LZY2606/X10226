package analysis

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"tga-evidence/internal/domain"
)

func numeric(id, original, unit, basis string) domain.ValueRef {
	value, err := strconv.ParseFloat(strings.TrimSpace(original), 64)
	ref := domain.ValueRef{ID: id, Original: original, Unit: unit, CoordinateBasis: basis, Status: "unknown"}
	if err != nil || math.IsNaN(value) {
		ref.Status = "unconvertible"
		ref.Diagnostics = []string{"无法转换为数值；原始表示保留在原位置"}
		return ref
	}
	ref.Value = &value
	ref.Status = "numeric"
	return ref
}

func valueID(runID, suffix string, index int) string {
	return fmt.Sprintf("val:%s:%s:%03d", runID, suffix, index)
}

func ptrFloat(v float64) *float64 { return &v }

func refFromFloat(id string, v float64, unit, basis string) domain.ValueRef {
	return domain.ValueRef{ID: id, Value: ptrFloat(v), Original: strconv.FormatFloat(v, 'g', -1, 64), Unit: unit, CoordinateBasis: basis, Status: "numeric"}
}

func unknownRef(id, original, unit, basis, reason string) domain.ValueRef {
	return domain.ValueRef{ID: id, Original: original, Unit: unit, CoordinateBasis: basis, Status: "unconvertible", Diagnostics: []string{reason}}
}

func cloneRefWithID(ref domain.ValueRef, id string) domain.ValueRef {
	ref.ID = id
	return ref
}
