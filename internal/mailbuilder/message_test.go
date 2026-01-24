package mailbuilder

import (
	"fmt"
	"testing"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/stretchr/testify/assert"
)

func TestBuildHeaders(t *testing.T) {
	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	baseHeaders := headers{
		"testH": {"testH"},
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders, sqlc.Headers{})

	if h["testH"][0] != "testH" {
		t.Errorf("baseHeaders did not propagaged: %v", baseHeaders)
	}
	if h["Subject"][0] != "test subject" {
		t.Errorf("Subject headers not correct: %v != %v", h["Subject"][0], "test subject")
	}

	expSender := fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)
	if h["From"][0] != expSender {
		t.Errorf("From headers not correct: %v != %v", h["From"][0], expSender)
	}

	if h["To"][0] != "to@email.com" {
		t.Errorf("To headers not correct: %v != %v", h["To"][0], "to@email.com")
	}
}

func TestBuildHeadersShouldCopyBaseHeader(t *testing.T) {
	baseHeaders := headers{
		"testH": {"testH"},
	}

	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders, sqlc.Headers{})
	if len(baseHeaders) != 1 {
		t.Errorf("base headers has changed")
	}
}

func TestBuildHeadersCustomTo(t *testing.T) {
	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	ch := sqlc.Headers{
		To: []string{"visible@example.com"},
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"visible@example.com"}, h["To"])
	value := h["Cc"]
	assert.Equal(t, []string{}, value)
}

func TestBuildHeadersWithCC(t *testing.T) {
	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	ch := sqlc.Headers{
		Cc: []string{"cc1@example.com", "cc2@example.com"},
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"to@email.com"}, h["To"])
	assert.Equal(t, []string{"cc1@example.com", "cc2@example.com"}, h["Cc"])
}

func TestBuildHeadersBothToAndCC(t *testing.T) {
	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	ch := sqlc.Headers{
		To: []string{"visible@example.com", "visible2@example.com"},
		Cc: []string{"cc@example.com"},
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"visible@example.com", "visible2@example.com"}, h["To"])
	assert.Equal(t, []string{"cc@example.com"}, h["Cc"])
}

func TestInsertTrackOpen(t *testing.T) {
	html := `<html><body></body></html>`
	exptected := `<html><body><img src="https://test.com/o/xxx" style="display:none;"/></body></html>`
	res := insertTrackLinkInHTML(html, "https://test.com/o/xxx")
	assert.Equal(t, exptected, res)
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

	expectedhtml := html

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})

	assert.Nil(t, err)
	assert.Equal(t, expectedhtml, res)
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
