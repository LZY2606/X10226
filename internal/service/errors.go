package service

import "errors"

var (
	ErrUnknownConflict = errors.New("unknown conflict")
	ErrConflictClosed  = errors.New("conflict already resolved")
	ErrUnknownEvent    = errors.New("unknown event")
)
