package mailbuilder

import (
	"testing"

	"kannon.gyozatech.dev/internal/db"
)

func TestBuildHeaders(t *testing.T) {

	sender := db.Sender{
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

	if h["From"] != sender.GetSender() {
		t.Errorf("From headers not correct: %v != %v", h["From"], sender.GetSender())
	}

	if h["To"] != "to@email.com" {
		t.Errorf("From headers not correct: %v != %v", h["To"], "to@email.com")
	}

}
