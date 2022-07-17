package utils

import (
	"fmt"
	"regexp"

	"github.com/lucsky/cuid"
)

var extractMsgIDReg = regexp.MustCompile(`<.+\/(?P<messageId>.+)>`)
var matchDomainReg = regexp.MustCompile(`.+@(?P<domain>.+)`)

func ExtractMsgIDAndDomain(messageID string) (msgID string, domain string, err error) {
	match := extractMsgIDReg.FindStringSubmatch(messageID)
	if len(match) != 2 {
		return "", "", fmt.Errorf("invalid messageID: %v", messageID)
	}
	msgID = match[1]

	matchDomain := matchDomainReg.FindStringSubmatch(msgID)
	if len(matchDomain) != 2 {
		return "", "", fmt.Errorf("invalid messageID: %v", messageID)
	}
	domain = matchDomain[1]
	return
}

func CreateMessageID(domain string) string {
	return fmt.Sprintf("msg_%v@%v", cuid.New(), domain)
}
