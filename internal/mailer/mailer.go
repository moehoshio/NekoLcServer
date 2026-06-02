// Package mailer provides a small SMTP client used for account-related emails
// (password recovery and email verification). It relies only on the Go standard
// library and degrades to a no-op when SMTP is not configured/enabled.
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

// ErrDisabled is returned when an email send is attempted while SMTP is disabled.
var ErrDisabled = errors.New("smtp is not enabled")

// Mailer sends email messages over SMTP using a configuration snapshot.
type Mailer struct {
	cfg config.SMTPConfig
}

// New builds a Mailer from the provided SMTP configuration.
func New(cfg config.SMTPConfig) *Mailer {
	return &Mailer{cfg: cfg}
}

// Enabled reports whether the mailer is configured to send email.
func (m *Mailer) Enabled() bool {
	return m != nil && m.cfg.Enabled && strings.TrimSpace(m.cfg.Host) != "" && strings.TrimSpace(m.cfg.From) != ""
}

// fromAddress returns the configured From address.
func (m *Mailer) fromAddress() string {
	return strings.TrimSpace(m.cfg.From)
}

// BuildMessage assembles a MIME message (headers + body) for the given recipient.
// It is exported to make message construction independently testable.
func (m *Mailer) BuildMessage(to, subject, body string) []byte {
	from := m.fromAddress()
	fromHeader := from
	if name := strings.TrimSpace(m.cfg.FromName); name != "" {
		fromHeader = fmt.Sprintf("%s <%s>", name, from)
	}
	var b strings.Builder
	b.WriteString("From: " + sanitizeHeader(fromHeader) + "\r\n")
	b.WriteString("To: " + sanitizeHeader(to) + "\r\n")
	b.WriteString("Subject: " + encodeHeader(sanitizeHeader(subject)) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// sanitizeHeader strips CR/LF (and NUL) characters from a header value to
// prevent SMTP header/email injection via attacker-controlled fields such as
// the recipient address or subject.
func sanitizeHeader(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', 0:
			return -1
		}
		return r
	}, s)
}

// encodeHeader RFC 2047 encodes a header value when it contains non-ASCII bytes.
func encodeHeader(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return mimeEncode(s)
		}
	}
	return s
}

func mimeEncode(s string) string {
	// Base64 "B" encoding per RFC 2047.
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	data := []byte(s)
	var enc strings.Builder
	for i := 0; i < len(data); i += 3 {
		var n uint32
		rem := len(data) - i
		n = uint32(data[i]) << 16
		if rem > 1 {
			n |= uint32(data[i+1]) << 8
		}
		if rem > 2 {
			n |= uint32(data[i+2])
		}
		enc.WriteByte(tbl[(n>>18)&0x3F])
		enc.WriteByte(tbl[(n>>12)&0x3F])
		if rem > 1 {
			enc.WriteByte(tbl[(n>>6)&0x3F])
		} else {
			enc.WriteByte('=')
		}
		if rem > 2 {
			enc.WriteByte(tbl[n&0x3F])
		} else {
			enc.WriteByte('=')
		}
	}
	return "=?UTF-8?B?" + enc.String() + "?="
}

// Send delivers a plaintext email to a single recipient.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		return ErrDisabled
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return errors.New("recipient is required")
	}
	port := m.cfg.Port
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprintf("%d", port))
	msg := m.BuildMessage(to, subject, body)

	var auth smtp.Auth
	if strings.TrimSpace(m.cfg.Username) != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	switch strings.ToLower(strings.TrimSpace(m.cfg.TLSMode)) {
	case "tls":
		return m.sendImplicitTLS(addr, auth, to, msg)
	default:
		// "none" or "starttls": connect plain, upgrade with STARTTLS when offered.
		return m.sendSTARTTLS(addr, auth, to, msg)
	}
}

func (m *Mailer) sendSTARTTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	return m.deliver(c, auth, to, msg)
}

func (m *Mailer) sendImplicitTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	return m.deliver(c, auth, to, msg)
}

func (m *Mailer) deliver(c *smtp.Client, auth smtp.Auth, to string, msg []byte) error {
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(m.fromAddress()); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(msg); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}
