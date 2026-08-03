package utils

import (
	"encoding/base64"
	"fmt"
	"regexp"
)

var extractMsgIDReg = regexp.MustCompile(`<.+\/(?P<messageId>.+)>`)
var matchDomainReg = regexp.MustCompile(`.+@(?P<domain>.+)`)

func ExtractDomainFromMessageID(messageID string) (domain string, err error) {
	match := matchDomainReg.FindStringSubmatch(messageID)
	if len(match) != 2 {
		return "", fmt.Errorf("invalid messageID: %v", messageID)
	}
	domain = match[1]
	return
}

func ExtractMsgIDAndDomainFromEmailID(emailID string) (msgID string, domain string, err error) {
	match := extractMsgIDReg.FindStringSubmatch(emailID)
	if len(match) != 2 {
		return "", "", fmt.Errorf("invalid emailID: %v", emailID)
	}
	msgID = match[1]

	domain, err = ExtractDomainFromMessageID(msgID)
	if err != nil {
		return "", "", err
	}
	return
}

var parseReturnPath = regexp.MustCompile(`bump_(?P<emailHash>[^+]*)\+(?P<messageID>.*)`)

func ParseBounceReturnPath(returnPath string) (email string, messageID string, domain string, found bool, err error) {
	match := parseReturnPath.FindStringSubmatch(returnPath)
	if match == nil {
		return "", "", "", false, nil
	}
	if len(match) != 3 {
		return "", "", "", false, fmt.Errorf("invalid returnPath: %v", returnPath)
	}
	emailHash := match[1]
	messageID = match[2]
	found = true

	// The email segment is encoded with the URL-safe alphabet by buildReturnPath
	// (internal/envelope/message.go) — '+' is the field separator in
	// "bump_<email>+<messageID>", so the standard alphabet, which can itself
	// emit '+', is not a valid choice for this segment. The decoder must match.
	emailBytes, err := base64.URLEncoding.DecodeString(emailHash)
	if err != nil {
		return "", "", "", false, fmt.Errorf("invalid returnPath: %w", err)
	}
	email = string(emailBytes)

	domain, err = ExtractDomainFromMessageID(messageID)
	if err != nil {
		return "", "", "", false, err
	}
	return
}
