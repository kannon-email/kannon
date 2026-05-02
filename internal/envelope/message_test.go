package envelope

import (
	"fmt"
	"testing"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/stretchr/testify/assert"
)

func TestBuildHeaders(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}

	baseHeaders := headers{"testH": {"testH"}}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders, batch.Headers{})

	assert.Equal(t, "testH", h["testH"][0])
	assert.Equal(t, "test subject", h["Subject"][0])
	assert.Equal(t, fmt.Sprintf("%v <%v>", sender.Alias, sender.Email), h["From"][0])
	assert.Equal(t, "to@email.com", h["To"][0])
}

func TestBuildHeadersShouldCopyBaseHeader(t *testing.T) {
	baseHeaders := headers{"testH": {"testH"}}
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}

	buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders, batch.Headers{})
	assert.Equal(t, 1, len(baseHeaders))
}

func TestBuildHeadersCustomTo(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}
	ch := batch.Headers{To: []string{"visible@example.com"}}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"visible@example.com"}, h["To"])
	_, hasCC := h["Cc"]
	assert.False(t, hasCC)
}

func TestBuildHeadersWithCC(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}
	ch := batch.Headers{Cc: []string{"cc1@example.com", "cc2@example.com"}}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"to@email.com"}, h["To"])
	assert.Equal(t, []string{"cc1@example.com", "cc2@example.com"}, h["Cc"])
}

func TestBuildHeadersBothToAndCC(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}
	ch := batch.Headers{
		To: []string{"visible@example.com", "visible2@example.com"},
		Cc: []string{"cc@example.com"},
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"visible@example.com", "visible2@example.com"}, h["To"])
	assert.Equal(t, []string{"cc@example.com"}, h["Cc"])
}

func TestInsertTrackOpen(t *testing.T) {
	html := `<html><body></body></html>`
	expected := `<html><body><img src="https://test.com/o/xxx" style="display:none;"/></body></html>`
	assert.Equal(t, expected, insertTrackLinkInHTML(html, "https://test.com/o/xxx"))
}

func TestInsertTrackLink(t *testing.T) {
	html := `<html>
<body>
<a href="http://link1.com" />
<a href="http://link2.com" />
<img src="https://test.com/o/xxx" style="display:none;"/>
</body></html>`

	expectedhtml := `<html>
<body>
<a href="http://link1.comx" />
<a href="http://link2.comx" />
<img src="https://test.com/o/xxx" style="display:none;"/>
</body></html>`

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, expectedhtml, res)
}

func TestEmptyAHrefLink(t *testing.T) {
	html := `<html>
<body>
<a href="" />
</body></html>`

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, html, res)
}

// https://github.com/kannon-email/kannon/issues/276
func TestIssue276Link(t *testing.T) {
	html := `<html>
<body>
<img src="https://google.com/test" />
<a href="https://google.com" />
</body></html>`

	expectedhtml := `<html>
<body>
<img src="https://google.com/test" />
<a href="https://google.comx" />
</body></html>`

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, expectedhtml, res)
}
