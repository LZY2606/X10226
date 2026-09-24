package engine

import "encoding/json"

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func recodeJSON(s string, v any) error    { return json.Unmarshal([]byte(s), v) }
