package utils

import (
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
