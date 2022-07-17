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

func TestInsertTrackLink(t *testing.T) {
	html := `<html><body></body></html>`
	exptectedHtml := `<html><body><img src="https://test.com/o/xxx" style="display:none;" /></body></html>`
	res := insertTrackLinkInHtml(html, "https://test.com/o/xxx")
	assert.Equal(t, exptectedHtml, res)
}
