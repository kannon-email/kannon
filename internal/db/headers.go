package sqlc

// Headers is the JSONB payload of messages.headers: everything a Batch states
// about the headers of its outgoing messages. Adding a key here needs no
// migration — rows written before the key existed simply decode without it.
type Headers struct {
	To []string `json:"to"`
	Cc []string `json:"cc"`
	// OneClickUnsubscribe is absent on every Batch written before ADR 0005, and
	// on every Batch whose caller states no unsubscribe endpoint.
	OneClickUnsubscribe *OneClickUnsubscribe `json:"one_click_unsubscribe,omitempty"`
}

// OneClickUnsubscribe is the stored form of the sender's unsubscribe endpoint.
type OneClickUnsubscribe struct {
	URLTemplate string `json:"url_template"`
}
