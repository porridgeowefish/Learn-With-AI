// Package maildelivery sends account verification codes through an SMTP relay.
package maildelivery

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type SMTP struct {
	Address     string
	From        string
	Username    string
	Password    string
	ImplicitTLS bool
	roots       *x509.CertPool
}

func (s SMTP) Validate() error {
	host, port, err := net.SplitHostPort(s.Address)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 ||
		!isAddress(s.From) || (s.Username == "") != (s.Password == "") {
		return errors.New("invalid email relay configuration")
	}
	return nil
}

func (s SMTP) SendCode(ctx context.Context, email, code string) error {
	if s.Validate() != nil || !isAddress(email) || len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return errors.New("invalid email relay configuration or recipient")
	}
	host, _, _ := net.SplitHostPort(s.Address)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dialer := &net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", s.Address)
	if err != nil {
		return err
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		return err
	}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: s.roots}
	var conn net.Conn = raw
	if s.ImplicitTLS {
		conn = tls.Client(raw, tlsConfig)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if !s.ImplicitTLS {
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(s.From); err != nil {
		return err
	}
	if err := client.Rcpt(email); err != nil {
		return err
	}
	body, err := client.Data()
	if err != nil {
		return err
	}
	sender := mail.Address{Name: "Learn-With-AI", Address: s.From}
	domain := s.From[strings.LastIndexByte(s.From, '@')+1:]
	_, writeErr := fmt.Fprintf(body,
		"From: %s\r\nTo: %s\r\nSubject: Learn-With-AI verification code\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour Learn-With-AI verification code is %s. It expires in 10 minutes.\r\n",
		sender.String(), email, time.Now().UTC().Format(time.RFC1123Z), rand.Text(), domain, code)
	if err := errors.Join(writeErr, body.Close()); err != nil {
		return err
	}
	// The relay has accepted the message at this point. QUIT errors should not
	// invalidate a code that might already be in the user's inbox.
	_ = client.Quit()
	return nil
}

func isAddress(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && !strings.ContainsAny(value, "\r\n")
}
