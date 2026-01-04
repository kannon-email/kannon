package utils

import (
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/lucsky/cuid"
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

func CreateMessageID(domain string) string {
	return fmt.Sprintf("msg_%v@%v", cuid.New(), domain)
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

	emailBytes, err := base64.StdEncoding.DecodeString(emailHash)
	if err != nil {
		return "", "", "", false, fmt.Errorf("invalid returnPath: %v", err)
	}
	email = string(emailBytes)

	domain, err = ExtractDomainFromMessageID(messageID)
	if err != nil {
		return "", "", "", false, err
	}
	return
}
