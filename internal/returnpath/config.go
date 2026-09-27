package returnpath

import (
	"fmt"

	"github.com/kannon-email/kannon/x/config"
)

// Load resolves the configured bounce secret into a Signer. One message for a secret that could
// not be resolved and one that is not usable, because the operator's next move is the same, and it
// is to look at this key.
func Load() (Signer, error) {
	raw, err := config.BounceSecret()
	if err == nil {
		var s Signer
		if s, err = Parse(raw); err == nil {
			return s, nil
		}
	}
	return Signer{}, fmt.Errorf(
		"bounce return paths are signed, and the dispatcher and the inbound SMTP server need the secret "+
			"to sign and verify them: set %q in the config file to a random value of at least %d characters, "+
			"the same on every process running either, taking it from the environment as "+
			"`secret: env://KANNON_BOUNCE_SECRET` if you would rather not write it there (%w)",
		config.BounceSecretKey, MinSecretLength, err)
}

// MustLoad is Load for a runnable's constructor, which runs on the boot path after the boot has
// already been refused for a missing secret — so a failure here is a wiring mistake, not an
// operator's.
func MustLoad() Signer {
	s, err := Load()
	if err != nil {
		panic(err)
	}
	return s
}
