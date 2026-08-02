package batch

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBatch(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		b, err := New(NewParams{Domain: "example.com", Subject: "subject", Sender: Sender{Email: "from@example.com", Alias: "From"}, TemplateID: "tpl_abc"})
		require.NoError(t, err)
		assert.False(t, b.ID().IsZero())
		assert.True(t, strings.HasPrefix(b.ID().String(), IDPrefix))
		assert.True(t, strings.HasSuffix(b.ID().String(), "@example.com"))
		assert.Equal(t, "subject", b.Subject())
		assert.Equal(t, "tpl_abc", b.TemplateID())
		assert.Equal(t, "example.com", b.Domain())
		assert.Equal(t, "from@example.com", b.Sender().Email)
	})

	t.Run("TracksStatedPolicy", func(t *testing.T) {
		stated := tracking.Policy{Opens: tracking.ModeFull}
		b, err := New(NewParams{Domain: "example.com", Subject: "subject", Sender: Sender{Email: "from@example.com", Alias: "From"}, TemplateID: "tpl_abc", Tracking: stated})
		require.NoError(t, err)
		assert.Equal(t, stated, b.TrackingPolicy(), "New must keep the stated Policy as-is, not normalise it")
	})

	t.Run("MissingDomain", func(t *testing.T) {
		_, err := New(NewParams{Subject: "subject", Sender: Sender{Email: "a@b.c"}, TemplateID: "tpl"})
		assert.Error(t, err)
	})

	t.Run("MissingSubject", func(t *testing.T) {
		_, err := New(NewParams{Domain: "d", Sender: Sender{Email: "a@b.c"}, TemplateID: "tpl"})
		assert.Error(t, err)
	})

	t.Run("MissingTemplateID", func(t *testing.T) {
		_, err := New(NewParams{Domain: "d", Subject: "s", Sender: Sender{Email: "a@b.c"}})
		assert.Error(t, err)
	})

	t.Run("MissingSenderEmail", func(t *testing.T) {
		_, err := New(NewParams{Domain: "d", Subject: "s", TemplateID: "tpl"})
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
		Tracking:    tracking.Policy{Opens: tracking.ModeFull},
	})
	assert.Equal(t, ID("msg_abc@d"), b.ID())
	assert.Equal(t, "s", b.Subject())
	assert.Equal(t, "e", b.Sender().Email)
	assert.Equal(t, "tpl", b.TemplateID())
	assert.Equal(t, "d", b.Domain())
	assert.Equal(t, []byte("hi"), b.Attachments()["file.txt"])
	assert.Equal(t, []string{"to@d"}, b.Headers().To)
	assert.Equal(t, tracking.Policy{Opens: tracking.ModeFull}, b.TrackingPolicy())
}

func TestNewBatchValidatesTheUnsubscribeEndpoint(t *testing.T) {
	newWith := func(tpl string) error {
		_, err := New(NewParams{
			Domain:              "example.com",
			Subject:             "subject",
			Sender:              Sender{Email: "from@example.com", Alias: "From"},
			TemplateID:          "tpl_abc",
			OneClickUnsubscribe: OneClickUnsubscribe{URLTemplate: tpl},
		})
		return err
	}

	t.Run("HTTPSTemplateAccepted", func(t *testing.T) {
		assert.NoError(t, newWith("https://test.com/unsub?email={{ email }}"))
	})

	t.Run("NoneStatedAccepted", func(t *testing.T) {
		assert.NoError(t, newWith(""))
	})

	t.Run("PlainHTTPRejected", func(t *testing.T) {
		// A one-click POST carries the recipient's identifier; http would put it
		// on the wire in the clear.
		assert.Error(t, newWith("http://test.com/unsub"))
	})

	t.Run("MailtoRejected", func(t *testing.T) {
		assert.Error(t, newWith("mailto:unsub@test.com"))
	})

	t.Run("RelativeRejected", func(t *testing.T) {
		assert.Error(t, newWith("/unsub"))
	})

	t.Run("HeaderInjectionRejected", func(t *testing.T) {
		assert.Error(t, newWith("https://test.com/unsub\r\nBcc: victim@test.com"))
	})
}

func TestBatchExposesTheUnsubscribeEndpoint(t *testing.T) {
	b, err := New(NewParams{
		Domain:              "example.com",
		Subject:             "subject",
		Sender:              Sender{Email: "from@example.com", Alias: "From"},
		TemplateID:          "tpl_abc",
		OneClickUnsubscribe: OneClickUnsubscribe{URLTemplate: "https://test.com/unsub"},
	})
	require.NoError(t, err)

	assert.Equal(t, "https://test.com/unsub", b.OneClickUnsubscribe().URLTemplate)
	assert.False(t, b.OneClickUnsubscribe().IsZero())
}
