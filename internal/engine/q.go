package engine

import (
	"tgareview/internal/compute"
	"tgareview/internal/domain"
)

func computeQValue(raw string, unit domain.Unit) domain.Q {
	return compute.ParseQ(raw, unit)
}
