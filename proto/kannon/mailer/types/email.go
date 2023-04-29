package types

import "github.com/networkteam/obfuscate"

func (ets *EmailToSend) GetObfuscatedTo() string {
	return obfuscate.EmailAddressPartially(ets.To)
}
