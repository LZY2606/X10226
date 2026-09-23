package domain

// Project is a review workspace. Identity is explicit and survives
// export -> delete -> import.
type Project struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedSeq int64  `json:"created_seq"`
	CreatedAt  int64  `json:"created_at"`
}
