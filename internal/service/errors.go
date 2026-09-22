package service

import "errors"

// Domain sentinel errors.
var (
	ErrNoSamples      = errors.New("cannot derive: samples layer missing")
	ErrTargetNotFound = errors.New("decision target not found in current derived node")
	ErrConflict       = errors.New("conflicting decision already exists")
)
