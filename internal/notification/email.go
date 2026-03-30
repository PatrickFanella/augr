package notification

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

type sendMailFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

// EmailNotifier delivers alerts via SMTP.
type EmailNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       []string
	sendMail sendMailFunc
}

// NewEmailNotifier returns an SMTP-backed notifier.
func NewEmailNotifier(host string, port int, username, password, from string, to []string) *EmailNotifier {
	return &EmailNotifier{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		to:       append([]string(nil), to...),
		sendMail: smtp.SendMail,
	}
}

// Notify sends an alert email to all configured recipients.
func (n *EmailNotifier) Notify(_ context.Context, alert Alert) error {
	addr := fmt.Sprintf("%s:%d", n.host, n.port)
	subject := alert.Title
	if strings.TrimSpace(subject) == "" {
		subject = "Alert"
	}

	message := strings.Join([]string{
		fmt.Sprintf("From: %s", n.from),
		fmt.Sprintf("To: %s", strings.Join(n.to, ", ")),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		formatAlertText(alert),
		"",
	}, "\r\n")

	var auth smtp.Auth
	if strings.TrimSpace(n.username) != "" {
		auth = smtp.PlainAuth("", n.username, n.password, n.host)
	}

	return n.sendMail(addr, auth, n.from, n.to, []byte(message))
}
