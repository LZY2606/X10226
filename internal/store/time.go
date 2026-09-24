package store

import "time"

func nowUnix() int64 { return time.Now().Unix() }

// FakeClock is a deterministic clock used by tests and scripted case packs.
type FakeClock struct{ T int64 }

func (f *FakeClock) Now() int64      { return f.T }
func (f *FakeClock) Advance(sec int64) { f.T += sec }
