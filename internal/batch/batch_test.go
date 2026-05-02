package batch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBatch(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		b, err := New("example.com", "subject", Sender{Email: "from@example.com", Alias: "From"}, "tpl_abc", nil, Headers{})
		require.NoError(t, err)
		assert.False(t, b.ID().IsZero())
		assert.True(t, strings.HasPrefix(b.ID().String(), IDPrefix))
		assert.True(t, strings.HasSuffix(b.ID().String(), "@example.com"))
		assert.Equal(t, "subject", b.Subject())
		assert.Equal(t, "tpl_abc", b.TemplateID())
		assert.Equal(t, "example.com", b.Domain())
		assert.Equal(t, "from@example.com", b.Sender().Email)
	})

	t.Run("MissingDomain", func(t *testing.T) {
		_, err := New("", "subject", Sender{Email: "a@b.c"}, "tpl", nil, Headers{})
		assert.Error(t, err)
	})

	t.Run("MissingSubject", func(t *testing.T) {
		_, err := New("d", "", Sender{Email: "a@b.c"}, "tpl", nil, Headers{})
		assert.Error(t, err)
	})

	t.Run("MissingTemplateID", func(t *testing.T) {
		_, err := New("d", "s", Sender{Email: "a@b.c"}, "", nil, Headers{})
		assert.Error(t, err)
	})

	t.Run("MissingSenderEmail", func(t *testing.T) {
		_, err := New("d", "s", Sender{}, "tpl", nil, Headers{})
		assert.Error(t, err)
	})
}

func TestParseID(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		id, err := ParseID("msg_abc123@example.com")
		require.NoError(t, err)
		assert.Equal(t, "msg_abc123@example.com", id.String())
	})

	t.Run("Empty", func(t *testing.T) {
		_, err := ParseID("")
		assert.Error(t, err)
	})

	t.Run("MissingPrefix", func(t *testing.T) {
		_, err := ParseID("abc@example.com")
		assert.Error(t, err)
	})

	t.Run("MissingDomain", func(t *testing.T) {
		_, err := ParseID("msg_abc")
		assert.Error(t, err)
	})
}

func TestLoad(t *testing.T) {
	b := Load(LoadParams{
		ID:          "msg_abc@d",
		Subject:     "s",
		Sender:      Sender{Email: "e", Alias: "a"},
		TemplateID:  "tpl",
		Domain:      "d",
		Attachments: Attachments{"file.txt": []byte("hi")},
		Headers:     Headers{To: []string{"to@d"}, Cc: []string{"cc@d"}},
	})
	assert.Equal(t, ID("msg_abc@d"), b.ID())
	assert.Equal(t, "s", b.Subject())
	assert.Equal(t, "e", b.Sender().Email)
	assert.Equal(t, "tpl", b.TemplateID())
	assert.Equal(t, "d", b.Domain())
	assert.Equal(t, []byte("hi"), b.Attachments()["file.txt"])
	assert.Equal(t, []string{"to@d"}, b.Headers().To)
}
