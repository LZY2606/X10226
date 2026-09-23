package domain

import "encoding/json"

func jsonUnmarshal(b []byte, v interface{}) error { return json.Unmarshal(b, v) }
