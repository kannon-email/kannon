package utils

import (
	"fmt"
	"regexp"

	"github.com/lucsky/cuid"
)

var extractMsgIDReg = regexp.MustCompile(`<.+\/(?P<messageId>.+)>`)
var matchDomainReg = regexp.MustCompile(`.+@(?P<domain>.+)`)

func ExtractMsgIDAndDomain(messageID string) (msgID string, domain string) {
	match := extractMsgIDReg.FindStringSubmatch(messageID)
	msgID = match[1]

	matchDomain := matchDomainReg.FindStringSubmatch(msgID)
	domain = matchDomain[1]
	return
}

func CreateMessageID(domain string) string {
	return fmt.Sprintf("msg_%v@%v", cuid.New(), domain)
}
