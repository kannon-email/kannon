package utils

import "github.com/networkteam/obfuscate"

func ObfuscateEmail(email string) string {
	return obfuscate.EmailAddressPartially(email)
}
