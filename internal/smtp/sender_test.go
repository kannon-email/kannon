package smtp

import (
	"fmt"
	"net"
	"net/textproto"
	"testing"
	"time"
)

func TestDeliverConnAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dataReply string
		quitReply string
		wantCode  uint32
	}{
		{name: "accepted_quit_rejected", dataReply: "250 queued", quitReply: "500 quit failed"},
		{name: "accepted_quit_dropped", dataReply: "250 queued"},
		{name: "accepted_quit_success", dataReply: "250 queued", quitReply: "221 bye"},
		{name: "data_rejected", dataReply: "451 try later", wantCode: 451},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			deadline := time.Now().Add(5 * time.Second)
			if err := client.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			if err := server.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				defer server.Close()
				done <- func() error {
					wire := textproto.NewConn(server)
					if err := wire.PrintfLine("220 mx.example ESMTP"); err != nil {
						return err
					}
					for _, step := range []struct{ command, reply string }{
						{"EHLO sender.example", "250 mx.example"},
						{"MAIL FROM:<from@example.com>", "250 ok"},
						{"RCPT TO:<to@example.com>", "250 ok"},
						{"DATA", "354 send message"},
					} {
						line, err := wire.ReadLine()
						if err != nil {
							return err
						}
						if line != step.command {
							return fmt.Errorf("command = %q, want %q", line, step.command)
						}
						if err := wire.PrintfLine("%s", step.reply); err != nil {
							return err
						}
					}
					body, err := wire.ReadDotBytes()
					if err != nil {
						return err
					}
					if string(body) != "Subject: regression\n\nhello\n" {
						return fmt.Errorf("unexpected DATA: %q", body)
					}
					if err := wire.PrintfLine("%s", tc.dataReply); err != nil {
						return err
					}
					if tc.wantCode != 0 {
						return nil
					}
					line, err := wire.ReadLine()
					if err != nil {
						return err
					}
					if line != "QUIT" {
						return fmt.Errorf("command = %q, want QUIT", line)
					}
					if tc.quitReply == "" {
						return nil
					}
					return wire.PrintfLine("%s", tc.quitReply)
				}()
			}()
			err := deliverConn(client, "from@example.com", "to@example.com", []byte("Subject: regression\r\n\r\nhello\r\n"), "mx.example", false, "sender.example")
			if serverErr := <-done; serverErr != nil {
				t.Fatal(serverErr)
			}
			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("accepted DATA must succeed despite QUIT failure: %v", err)
				}
			} else if err == nil || err.Code() != tc.wantCode || err.IsPermanent() {
				t.Fatalf("want transient SMTP %d, got %#v", tc.wantCode, err)
			}
		})
	}
}
