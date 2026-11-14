package smtp

import (
	"context"
	"net"
	"net/netip"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestSendRefusesLoopbackMX(t *testing.T) {
	mx := startFakeMX(t)
	dns := startFakeDNS(t)
	dns.setMX("loop.test.", "mx.loop.test.")
	dns.setA("mx.loop.test.", "127.0.0.1")

	err := newTestSender(dns, mx, false).Send("from@sender.test", "to@loop.test", []byte("Subject: x\r\n\r\nhi\r\n"))

	assertRefusedLoopback(t, err)
	if n := mx.sessions.Load(); n != 0 {
		t.Fatalf("fake MX received %d sessions, want none", n)
	}
}

func TestSendRefusesLoopbackARecordFallback(t *testing.T) {
	mx := startFakeMX(t)
	dns := startFakeDNS(t)
	dns.setA("nomx.test.", "127.0.0.1")

	err := newTestSender(dns, mx, false).Send("from@sender.test", "to@nomx.test", []byte("Subject: x\r\n\r\nhi\r\n"))

	assertRefusedLoopback(t, err)
	if n := mx.sessions.Load(); n != 0 {
		t.Fatalf("fake MX received %d sessions, want none", n)
	}
}

func TestSendRefusesLoopbackAddressLiteral(t *testing.T) {
	for _, to := range []string{"to@127.0.0.1", "to@[127.0.0.1]"} {
		t.Run(to, func(t *testing.T) {
			mx := startFakeMX(t)
			dns := startFakeDNS(t)

			err := newTestSender(dns, mx, false).Send("from@sender.test", to, []byte("Subject: x\r\n\r\nhi\r\n"))

			if err == nil || !err.IsPermanent() {
				t.Fatalf("Send to an address literal = %v, want a permanent refusal", err)
			}
			if n := mx.sessions.Load(); n != 0 {
				t.Fatalf("fake MX received %d sessions, want none", n)
			}
		})
	}
}

// The first answer for the MX host is public, every later one is loopback: a
// check made on the looked-up name would pass and the dial would land on the
// listener.
func TestSendRefusesDestinationReboundToLoopback(t *testing.T) {
	mx := startFakeMX(t)
	dns := startFakeDNS(t)
	dns.setMX("rebind.test.", "mx.rebind.test.")
	dns.setA("mx.rebind.test.", "192.0.2.25")
	dns.rebind("mx.rebind.test.", 1, "127.0.0.1")
	s := newTestSender(dns, mx, false)
	looked, err := s.resolver.LookupHost(t.Context(), "mx.rebind.test")
	if err != nil || len(looked) != 1 || looked[0] != "192.0.2.25" {
		t.Fatalf("lookup = %v, %v; want the public answer first", looked, err)
	}

	sendErr := s.Send("from@sender.test", "to@rebind.test", []byte("Subject: x\r\n\r\nhi\r\n"))

	assertRefusedLoopback(t, sendErr)
	if n := mx.sessions.Load(); n != 0 {
		t.Fatalf("fake MX received %d sessions, want none", n)
	}
}

func TestSendDeliversToPrivateDestinationWhenAllowed(t *testing.T) {
	mx := startFakeMX(t)
	dns := startFakeDNS(t)
	dns.setMX("relay.test.", "mx.relay.test.")
	dns.setA("mx.relay.test.", "127.0.0.1")

	err := newTestSender(dns, mx, true).Send("from@sender.test", "to@relay.test", []byte("Subject: x\r\n\r\nhi\r\n"))

	if err != nil {
		t.Fatalf("Send with private destinations allowed: %v", err)
	}
	if got := mx.messages(); len(got) != 1 || got[0] != "Subject: x\n\nhi\n" {
		t.Fatalf("fake MX received %q, want the one message", got)
	}
}

func newTestSender(dns *fakeDNS, mx *fakeMX, allowPrivate bool) *sender {
	return &sender{Hostname: "sender.test", resolver: dns.resolver(), port: mx.port, allowPrivate: allowPrivate}
}

func assertRefusedLoopback(t *testing.T, err SenderError) {
	t.Helper()
	if err == nil {
		t.Fatal("Send succeeded, want refusal of a private destination")
	}
	if !err.IsPermanent() {
		t.Fatalf("refusal must not be retried, got transient error: %v", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error %q does not name the blocked destination 127.0.0.1", err.Error())
	}
}

type fakeMX struct {
	port     string
	sessions atomic.Int32
	mu       sync.Mutex
	received []string
}

// startFakeMX listens on loopback and accepts whatever one SMTP transaction sends.
func startFakeMX(t *testing.T) *fakeMX {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	mx := &fakeMX{port: port}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mx.sessions.Add(1)
			go mx.serve(conn)
		}
	}()
	return mx
}

func (mx *fakeMX) serve(conn net.Conn) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	wire := textproto.NewConn(conn)
	if err := wire.PrintfLine("220 mx.test ESMTP"); err != nil {
		return
	}
	for {
		line, err := wire.ReadLine()
		if err != nil {
			return
		}
		reply := "250 ok"
		switch strings.ToUpper(line) {
		case "DATA":
			if err := wire.PrintfLine("354 go ahead"); err != nil {
				return
			}
			body, err := wire.ReadDotBytes()
			if err != nil {
				return
			}
			mx.mu.Lock()
			mx.received = append(mx.received, string(body))
			mx.mu.Unlock()
			reply = "250 queued"
		case "QUIT":
			reply = "221 bye"
		}
		if err := wire.PrintfLine("%s", reply); err != nil || reply == "221 bye" {
			return
		}
	}
}

func (mx *fakeMX) messages() []string {
	mx.mu.Lock()
	defer mx.mu.Unlock()
	return append([]string(nil), mx.received...)
}

// fakeDNS is an in-process UDP DNS server; a.rebindAfter[name] switches a
// name's A answer to rebindTo once that many queries for it have been answered.
type fakeDNS struct {
	addr        string
	mu          sync.Mutex
	mx          map[string]string
	a           map[string][]netip.Addr
	rebindAfter map[string]int
	rebindTo    map[string][]netip.Addr
	queries     map[string]int
}

func startFakeDNS(t *testing.T) *fakeDNS {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	d := &fakeDNS{
		addr:        pc.LocalAddr().String(),
		mx:          map[string]string{},
		a:           map[string][]netip.Addr{},
		rebindAfter: map[string]int{},
		rebindTo:    map[string][]netip.Addr{},
		queries:     map[string]int{},
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			resp, ok := d.answer(buf[:n])
			if !ok {
				continue
			}
			if _, err := pc.WriteTo(resp, from); err != nil {
				return
			}
		}
	}()
	return d
}

func (d *fakeDNS) setMX(name, host string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mx[name] = host
}

func (d *fakeDNS) setA(name string, addrs ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.a[name] = parseAddrs(addrs)
}

func (d *fakeDNS) rebind(name string, after int, addrs ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rebindAfter[name] = after
	d.rebindTo[name] = parseAddrs(addrs)
}

func parseAddrs(addrs []string) []netip.Addr {
	parsed := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		parsed = append(parsed, netip.MustParseAddr(a))
	}
	return parsed
}

func (d *fakeDNS) resolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", d.addr)
		},
	}
}

func (d *fakeDNS) answer(req []byte) ([]byte, bool) {
	var query dnsmessage.Message
	if err := query.Unpack(req); err != nil || len(query.Questions) != 1 {
		return nil, false
	}
	q := query.Questions[0]
	name := strings.ToLower(q.Name.String())

	d.mu.Lock()
	defer d.mu.Unlock()

	resp := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true},
		Questions: query.Questions,
	}
	rh := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET}
	found := false
	switch q.Type {
	case dnsmessage.TypeMX:
		if host, ok := d.mx[name]; ok {
			found = true
			resp.Answers = append(resp.Answers, dnsmessage.Resource{Header: rh, Body: &dnsmessage.MXResource{Pref: 10, MX: dnsmessage.MustNewName(host)}})
		}
	case dnsmessage.TypeA:
		addrs, ok := d.a[name]
		if ok {
			found = true
			d.queries[name]++
			if after, rebinds := d.rebindAfter[name]; rebinds && d.queries[name] > after {
				addrs = d.rebindTo[name]
			}
			for _, a := range addrs {
				resp.Answers = append(resp.Answers, dnsmessage.Resource{Header: rh, Body: &dnsmessage.AResource{A: a.As4()}})
			}
		}
	case dnsmessage.TypeAAAA:
		_, found = d.a[name]
	}
	if !found {
		resp.RCode = dnsmessage.RCodeNameError
	}
	packed, err := resp.Pack()
	return packed, err == nil
}
