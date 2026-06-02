package mailer

import (
	"strings"
	"testing"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

func TestEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.SMTPConfig
		want bool
	}{
		{"disabled flag", config.SMTPConfig{Enabled: false, Host: "h", From: "a@b"}, false},
		{"missing host", config.SMTPConfig{Enabled: true, From: "a@b"}, false},
		{"missing from", config.SMTPConfig{Enabled: true, Host: "h"}, false},
		{"ok", config.SMTPConfig{Enabled: true, Host: "h", From: "a@b"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := New(tc.cfg).Enabled(); got != tc.want {
				t.Fatalf("Enabled()=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSendDisabledReturnsErr(t *testing.T) {
	m := New(config.SMTPConfig{})
	if err := m.Send("x@y.com", "s", "b"); err != ErrDisabled {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestBuildMessage(t *testing.T) {
	m := New(config.SMTPConfig{From: "noreply@example.com", FromName: "Neko"})
	msg := string(m.BuildMessage("user@example.com", "Reset your password", "Hello"))
	for _, want := range []string{
		"From: Neko <noreply@example.com>\r\n",
		"To: user@example.com\r\n",
		"Subject: Reset your password\r\n",
		"Content-Type: text/plain; charset=\"utf-8\"\r\n",
		"\r\nHello",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildMessageEncodesNonASCIISubject(t *testing.T) {
	m := New(config.SMTPConfig{From: "noreply@example.com"})
	msg := string(m.BuildMessage("user@example.com", "驗證您的電子郵件", "body"))
	if !strings.Contains(msg, "Subject: =?UTF-8?B?") {
		t.Fatalf("expected encoded subject, got:\n%s", msg)
	}
}
