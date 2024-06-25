package mailbuilder

import (
	"fmt"
	"testing"

	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/stretchr/testify/assert"
)

func TestBuildHeaders(t *testing.T) {
	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	baseHeaders := headers{
		"testH": "testH",
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders)

	if h["testH"] != "testH" {
		t.Errorf("baseHeaders did not propagaged: %v", baseHeaders)
	}
	if h["Subject"] != "test subject" {
		t.Errorf("Subject headers not correct: %v != %v", h["Subject"], "test subject")
	}

	expSender := fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)
	if h["From"] != expSender {
		t.Errorf("From headers not correct: %v != %v", h["From"], expSender)
	}

	if h["To"] != "to@email.com" {
		t.Errorf("From headers not correct: %v != %v", h["To"], "to@email.com")
	}
}

func TestBuildHeadersShouldCopyBaseHeader(t *testing.T) {
	baseHeaders := headers{
		"testH": "testH",
	}

	sender := pool.Sender{
		Email: "from@email.com",
		Alias: "email",
	}

	buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders)
	if len(baseHeaders) != 1 {
		t.Errorf("base headers has changed")
	}
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
