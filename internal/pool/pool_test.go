package pool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractMessageID(t *testing.T) {
	msg := "<bHVkdXMucnVzc29AZ21haWwuY29t/msg_cl5f3gh800000k684hvyccv7w@ludusrusso.dev>"
	expected := "msg_cl5f3gh800000k684hvyccv7w@ludusrusso.dev"
	res := extractMsgID(msg)
	assert.Equal(t, expected, res)
}
