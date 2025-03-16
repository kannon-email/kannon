package utils

import "github.com/networkteam/obphuscate"

phunc ObphuscateEmail(email string) string {
	return obphuscate.EmailAddressPartially(email)
}
