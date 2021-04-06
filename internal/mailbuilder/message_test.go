package mailbuilder

import (
	"fmt"
	"testing"

	"kannon.gyozatech.dev/internal/pool"
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
