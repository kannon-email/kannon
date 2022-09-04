package utils_test

import (
	"testing"

	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestExtractMessageID(t *testing.T) {
	msg := "<bHVkdXMucnVzc29AZ21haWwuY29t/msg_cl5f3gh800000k684hvyccv8w@test.dev>"
	expected := "msg_cl5f3gh800000k684hvyccv8w@test.dev"
	id, domain, err := utils.ExtractMsgIDAndDomainFromEmailID(msg)
	assert.Equal(t, expected, id)
	assert.Equal(t, "test.dev", domain)
	assert.Nil(t, err)
}

func TestExtractMsgIDAndDomainErr(t *testing.T) {
	msg := "msg_cl5f3gh800000k684hvyccv7w@test.dev"
	_, _, err := utils.ExtractMsgIDAndDomainFromEmailID(msg)
	assert.NotNil(t, err)
}

func TestParseBounceReturnPathNoBounce(t *testing.T) {
	returnPath := "xxx@test.com"
	//nolint:dogsled
	_, _, _, found, err := utils.ParseBounceReturnPath(returnPath)
	assert.Nil(t, err)
	assert.False(t, found)
}

func TestParseBounceReturnPath(t *testing.T) {
	returnPath := "bump_dGVzdEB0ZXN0LmNvbQ==+msg_cl6g7ndft0001018ut5octeun@k.test.com"
	email, messageID, domain, found, err := utils.ParseBounceReturnPath(returnPath)
	assert.Nil(t, err)
	assert.True(t, found)
	assert.Equal(t, "k.test.com", domain)
	assert.Equal(t, "test@test.com", email)
	assert.Equal(t, "msg_cl6g7ndft0001018ut5octeun@k.test.com", messageID)
}
