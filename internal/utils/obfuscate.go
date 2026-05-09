package utils

import "strings"

// ObfuscateEmail masks the local part of an email, keeping its first
// character and replacing the rest with '*' (one per character). The
// domain is left untouched. Inputs without '@' or with an empty local
// part are returned unchanged.
func ObfuscateEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at+1:]
	return string(local[0]) + strings.Repeat("*", len(local)-1) + "@" + domain
}
