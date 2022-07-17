package utils_test

import (
	"testing"

	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestExtractMessageID(t *testing.T) {
	msg := "<bHVkdXMucnVzc29AZ21haWwuY29t/msg_cl5f3gh800000k684hvyccv7w@test.dev>"
	expected := "msg_cl5f3gh800000k684hvyccv7w@test.dev"
	id, domain, err := utils.ExtractMsgIDAndDomain(msg)
	assert.Equal(t, expected, id)
	assert.Equal(t, "test.dev", domain)
	assert.Nil(t, err)
}

func TestExtractMsgIDAndDomainErr(t *testing.T) {
	msg := "msg_cl5f3gh800000k684hvyccv7w@test.dev"
	_, _, err := utils.ExtractMsgIDAndDomain(msg)
	assert.NotNil(t, err)
}
