package email

import (
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

type Config struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

func Send(cfg Config, to, subject, body string) error {
	if to == "" {
		return nil
	}
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == "" {
		port = "587"
	}
	from := cfg.From
	if from == "" {
		from = "burnrate@localhost"
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		from, to, subject, body)

	addr := net.JoinHostPort(host, port)

	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, host)
	}

	return smtp.SendMail(addr, auth, from, strings.Split(to, ","), []byte(msg))
}
