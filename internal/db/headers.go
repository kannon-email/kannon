package sqlc

type Headers struct {
	To []string `json:"to"`
	Cc []string `json:"cc"`
}
