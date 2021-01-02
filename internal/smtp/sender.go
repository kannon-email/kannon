package smtp

/*
	Internal error codes from
	https://serversmtp.com/it/errore-smtp/
*/

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"time"

	log "github.com/sirupsen/logrus"
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
	code        int
}

func (e smtpError) Error() string {
	return e.err.Error()
}

func (e smtpError) IsPermanent() bool {
	return e.isPermanent
}

func (e smtpError) Code() int {
	return e.code
}

func newSMTPError(err error, isPermanent bool, code int) *smtpError {
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
	toDomain, err := GetEmailDomain(to)
	log.Printf("domain %v\n", toDomain)
	if err != nil {
		// CHECK: 510: indiritto email errato
		return newSMTPError(err, true, 510)
	}

	mxs, lerr := lookupMXs(toDomain)
	if err != nil {
		return lerr
	}

	var lastErr *smtpError
	for _, mx := range mxs {
		err := deliver(from, to, msg, mx, false, s.Hostname)
		if err == nil {
			return nil
		}
		if err.IsPermanent() {
			return err
		}
		lastErr = err
	}
	err = fmt.Errorf("all MXs failed, last error: %v", lastErr)
	return newSMTPError(err, false, lastErr.Code())
}

func deliver(from, to string, msg []byte, mx string, insecure bool, domain string) *smtpError {
	smtpURL := fmt.Sprintf("%v:%v", mx, smtpPort)
	conn, err := net.DialTimeout("tcp", smtpURL, smtpDialTimeout)
	if err != nil {
		log.Debugf("Could not dial: %v", err)
		// TODO: add error code
		// Cannot dial SMTP 111
		return newSMTPError(err, false, 111)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(smtpTotalTimeout))

	c, err := smtp.NewClient(conn, mx)
	if err != nil {
		log.Debugf("Error creating client: %v", err)
		// TODO: add error code
		return newSMTPError(err, false, 111)
	}

	if err = c.Hello(domain); err != nil {
		log.Debugf("Error saying hello: %v", err)
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
				log.Debugf("TLS error: %v", err)
				// TODO: add error code
				return newSMTPError(err, false, 111)
			}
			log.Debugf("TLS error, retrying insecurely\n")
			return deliver(from, to, msg, mx, true, domain)
		}
	}

	if err := c.Mail(from); err != nil {
		log.Debugf("err: %v\n", err)
		return newSMTPErrorFromSTMP(err)
	}

	if err := c.Rcpt(to); err != nil {
		log.Debugf("err: %v\n", err)
		return newSMTPErrorFromSTMP(err)
	}

	w, err := c.Data()
	if err != nil {
		log.Debugf("err: %v\n", err)
		return newSMTPErrorFromSTMP(err)
	}
	_, err = w.Write(msg)
	if err != nil {
		log.Debugf("err: %v\n", err)
		return newSMTPErrorFromSTMP(err)
	}

	err = w.Close()
	if err != nil {
		log.Debugf("err: %v\n", err)
		return newSMTPErrorFromSTMP(err)
	}

	c.Quit()

	return nil
}

func lookupMXs(domain string) ([]string, *smtpError) {
	domain, err := idna.ToASCII(domain)
	if err != nil {
		// TODO: add error code
		return nil, newSMTPError(err, true, 512)
	}

	mxs := []string{}

	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		// TODO: Better handle Temporary errors.
		dnsErr, ok := err.(*net.DNSError)
		if !ok || !dnsErr.IsNotFound {
			log.Debugf("MX lookup error: %v", err)
			// TODO: add error code
			return nil, newSMTPError(dnsErr, !dnsErr.Temporary(), 512)
		}
		// Permanent error, we assume MX does not exist and fall back to A.
		log.Debugf("failed to resolve MX for %s, falling back to A", domain)
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

	log.Debugf("MXs: %v", mxs)
	return mxs, nil
}

func newSMTPErrorFromSTMP(err error) *smtpError {
	terr, ok := err.(*textproto.Error)
	if !ok {
		// unknown error
		return newSMTPError(err, false, 0)
	}

	return newSMTPError(err, terr.Code >= 500 && terr.Code < 600, terr.Code)
}
