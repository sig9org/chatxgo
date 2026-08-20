package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

type emailSender struct{ cfg EmailConfig }

func newEmailSender(cfg EmailConfig) *emailSender { return &emailSender{cfg: cfg} }
func (s *emailSender) Name() string               { return "email" }

func emailEnabled(c EmailConfig) bool {
	return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != "" && len(c.To)+len(c.Cc)+len(c.Bcc) > 0
}

func emailAddresses(values []string) ([]string, error) {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			a, err := mail.ParseAddress(part)
			if err != nil || a.Address == "" {
				return nil, fmt.Errorf("invalid email address %q", part)
			}
			out = append(out, a.Address)
		}
	}
	return out, nil
}

func emailHeaderAddresses(values []string) string {
	return strings.Join(values, ", ")
}

func emailMessage(cfg EmailConfig, msg Message) ([]byte, []string, error) {
	from, err := mail.ParseAddress(strings.TrimSpace(cfg.From))
	if err != nil {
		return nil, nil, fmt.Errorf("email: invalid EMAIL_FROM: %w", err)
	}
	to, err := emailAddresses(cfg.To)
	if err != nil {
		return nil, nil, fmt.Errorf("email: To: %w", err)
	}
	cc, err := emailAddresses(cfg.Cc)
	if err != nil {
		return nil, nil, fmt.Errorf("email: Cc: %w", err)
	}
	bcc, err := emailAddresses(cfg.Bcc)
	if err != nil {
		return nil, nil, fmt.Errorf("email: Bcc: %w", err)
	}
	if len(to)+len(cc)+len(bcc) == 0 {
		return nil, nil, errors.New("email: at least one recipient is required")
	}
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from.String())
	b.WriteString("\r\n")
	if len(to) > 0 {
		b.WriteString("To: ")
		b.WriteString(emailHeaderAddresses(to))
		b.WriteString("\r\n")
	}
	if len(cc) > 0 {
		b.WriteString("Cc: ")
		b.WriteString(emailHeaderAddresses(cc))
		b.WriteString("\r\n")
	}
	b.WriteString("Subject: ")
	b.WriteString(mime.QEncoding.Encode("UTF-8", msg.Subject))
	b.WriteString("\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(msg.Body)
	b.WriteString("\r\n")
	return []byte(b.String()), append(append(to, cc...), bcc...), nil
}

func (s *emailSender) Send(ctx context.Context, msg Message) error {
	c := s.cfg
	if strings.TrimSpace(c.Host) == "" || strings.TrimSpace(c.From) == "" {
		return errors.New("email: EMAIL_SMTP_HOST and EMAIL_FROM are required")
	}
	if c.Port == 0 {
		c.Port = 25
	}
	body, recipients, err := emailMessage(c, msg)
	if err != nil {
		return err
	}
	host, port := c.Host, c.Port
	if h, p, err := net.SplitHostPort(host); err == nil {
		host, port = h, pInt(p, port)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("email: connect SMTP server: %w", err)
	}
	defer conn.Close()
	var client *smtp.Client
	if port == 465 {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("email: TLS handshake: %w", err)
		}
		client, err = smtp.NewClient(tlsConn, host)
	} else {
		client, err = smtp.NewClient(conn, host)
	}
	if err != nil {
		return fmt.Errorf("email: SMTP handshake: %w", err)
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok && port != 465 {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: STARTTLS: %w", err)
		}
	} else if port == 587 {
		return errors.New("email: SMTP port 587 requires STARTTLS")
	}
	if c.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("email: SMTP server does not support AUTH")
		}
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, host)); err != nil {
			return fmt.Errorf("email: SMTP authentication: %w", err)
		}
	}
	if err := client.Mail(fromAddress(c.From)); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("email: RCPT TO %s: %w", recipient, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err = w.Write(body); err != nil {
		return fmt.Errorf("email: write message: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("email: finish message: %w", err)
	}
	return client.Quit()
}

func fromAddress(value string) string { a, _ := mail.ParseAddress(value); return a.Address }
func pInt(value string, fallback int) int {
	p, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return p
}
