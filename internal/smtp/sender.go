package smtp

/*
   Internal error codes from
   https://serversmtp.com/it/errore-smtp/
*/

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"net/textproto"
	"time"

	"golang.org/x/net/idna"
)

const (
	// Timeouts for SMTP delivery.
	smtpDialTimeout  = 15 * time.Second
	smtpTotalTimeout = 120 * time.Second

	// SMTP default port
	smtpPort = "25"
)

type smtpError struct {
	err         error
	isPermanent bool
	code        uint32
}

func (e smtpError) Error() string {
	return e.err.Error()
}

func (e smtpError) IsPermanent() bool {
	return e.isPermanent
}

func (e smtpError) Code() uint32 {
	return e.code
}

func newSMTPError(err error, isPermanent bool, code uint32) *smtpError {
	return &smtpError{
		err:         err,
		isPermanent: isPermanent,
		code:        code,
	}
}

type sender struct {
	Hostname string
}

// SenderName implements sender name function
// for Sender interface
func (s *sender) SenderName() string {
	return s.Hostname
}

// Send email
func (s *sender) Send(from, to string, msg []byte) SenderError {
	ctx, cancel := context.WithTimeout(context.Background(), smtpTotalTimeout)
	defer cancel()
	toDomain, err := GetEmailDomain(to)
	slog.Info(fmt.Sprintf("domain %v\n", toDomain))
	if err != nil {
		// CHECK: 510: indiritto email errato
		return newSMTPError(err, true, 510)
	}

	mxs, lerr := lookupMXsContext(ctx, toDomain)
	if lerr != nil {
		return lerr
	}

	if len(mxs) == 0 {
		return newSMTPError(errors.New("recipient domain has null MX"), true, 550)
	}

	var lastErr *smtpError
	for _, mx := range mxs {
		err := deliver(ctx, from, to, msg, mx, false, s.Hostname)
		if err == nil {
			return nil
		}
		if err.code > 200 {
			slog.Info(fmt.Sprintf("Error sending email to %v, cannot retry other MXs", to), "err", err)
			return err
		}

		lastErr = err
	}
	err = fmt.Errorf("all MXs failed, last error: %w", *lastErr)
	return newSMTPError(err, false, lastErr.Code())
}

func deliver(ctx context.Context, from, to string, msg []byte, mx string, insecure bool, domain string) *smtpError {
	smtpURL := fmt.Sprintf("%v:%v", mx, smtpPort)
	conn, err := (&net.Dialer{Timeout: smtpDialTimeout}).DialContext(ctx, "tcp", smtpURL)
	if err != nil {
		slog.Debug(fmt.Sprintf("Could not dial: %v", err))
		// TODO: add error code
		// Cannot dial SMTP 111
		return newSMTPError(err, false, 111)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		slog.Debug(fmt.Sprintf("Cannot set deadline: %v", err))
		// TODO: add error code
		return newSMTPError(err, false, 111)
	}

	return deliverConn(ctx, conn, from, to, msg, mx, insecure, domain)
}

// deliverConn runs the SMTP exchange on an established connection.
func deliverConn(ctx context.Context, conn net.Conn, from, to string, msg []byte, mx string, insecure bool, domain string) *smtpError {
	c, err := smtp.NewClient(conn, mx)
	if err != nil {
		slog.Debug(fmt.Sprintf("Error creating client: %v", err))
		// TODO: add error code
		return newSMTPError(err, false, 111)
	}

	if err = c.Hello(domain); err != nil {
		slog.Debug(fmt.Sprintf("Error saying hello: %v", err))
		// TODO: add error code
		return newSMTPError(err, false, 111)
	}

	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{
			ServerName:         mx,
			InsecureSkipVerify: insecure,
		}
		err = c.StartTLS(config)
		if err != nil {
			// Unfortunately, many servers use self-signed certs, so if we
			// fail verification we just try again without validating.
			if insecure {
				slog.Debug(fmt.Sprintf("TLS error: %v", err))
				// TODO: add error code
				return newSMTPError(err, false, 111)
			}
			slog.Debug("TLS error, retrying insecurely\n")
			return deliver(ctx, from, to, msg, mx, true, domain)
		}
	}

	if err := c.Mail(from); err != nil {
		slog.Debug(fmt.Sprintf("err: %v\n", err))
		return newSMTPErrorFromSTMP(err)
	}

	if err := c.Rcpt(to); err != nil {
		slog.Debug(fmt.Sprintf("err: %v\n", err))
		return newSMTPErrorFromSTMP(err)
	}

	w, err := c.Data()
	if err != nil {
		slog.Debug(fmt.Sprintf("err: %v\n", err))
		return newSMTPErrorFromSTMP(err)
	}
	_, err = w.Write(msg)
	if err != nil {
		slog.Debug(fmt.Sprintf("err: %v\n", err))
		return newSMTPErrorFromSTMP(err)
	}

	err = w.Close()
	if err != nil {
		slog.Debug(fmt.Sprintf("err: %v\n", err))
		return newSMTPErrorFromSTMP(err)
	}

	// DATA was accepted. A failed QUIT must not trigger a duplicate delivery.
	if err := c.Quit(); err != nil {
		slog.Debug("QUIT failed after DATA acceptance", "err", err)
	}

	return nil
}

func lookupMXsContext(ctx context.Context, domain string) ([]string, *smtpError) {
	domain, err := idna.ToASCII(domain)
	if err != nil {
		// TODO: add error code
		return nil, newSMTPError(err, true, 512)
	}

	mxs := []string{}

	mxRecords, err := net.DefaultResolver.LookupMX(ctx, domain)
	if err != nil {
		if ctx.Err() != nil {
			return nil, newSMTPError(ctx.Err(), false, 512)
		}
		// TODO: Better handle Temporary errors.
		dnsErr := &net.DNSError{}
		ok := errors.As(err, &dnsErr)
		if !ok || !dnsErr.IsNotFound {
			slog.Debug(fmt.Sprintf("MX lookup error: %v", err))
			// TODO: add error code
			return nil, newSMTPError(dnsErr, !dnsErr.Temporary(), 512)
		}
		// Permanent error, we assume MX does not exist and fall back to A.
		slog.Debug(fmt.Sprintf("failed to resolve MX for %s, falling back to A", domain))
		mxs = []string{domain}
	} else {
		// Convert the DNS records to a plain string slice. They're already
		// sorted by priority.
		for _, r := range mxRecords {
			mxs = append(mxs, r.Host)
		}
	}

	// Note that mxs could be empty; in that case we do NOT fall back to A.
	// This case is explicitly covered by the SMTP RFC.
	// https://tools.ietf.org/html/rfc5321#section-5.1

	// Cap the list of MXs to 5 hosts, to keep delivery attempt times
	// sane and prevent abuse.
	if len(mxs) > 5 {
		mxs = mxs[:5]
	}

	slog.Debug(fmt.Sprintf("MXs: %v", mxs))
	return mxs, nil
}

func newSMTPErrorFromSTMP(err error) *smtpError {
	terr := &textproto.Error{}
	ok := errors.As(err, &terr)
	if !ok {
		// unknown error
		return newSMTPError(err, false, 0)
	}

	isPermanent := terr.Code >= 500 && terr.Code < 600

	return newSMTPError(err, isPermanent, uint32(terr.Code))
}
