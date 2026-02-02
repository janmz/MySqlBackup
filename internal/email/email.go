// Package email sends error notifications via SMTP.
package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/janmz/mysqlbackup/internal/config"
)

// Send sends an email to admin_email with the given subject and body (plain text).
// admin_smtp_tls: "tls" = implizites TLS (Port 465), "starttls" = STARTTLS (Port 587), "" = Auto (465→tls, 587→starttls).
func Send(cfg *config.Config, subject, body string) error {
	if cfg.AdminEmail == "" || cfg.AdminSMTPServer == "" {
		return nil
	}
	port := cfg.AdminSMTPPort
	if port <= 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", cfg.AdminSMTPServer, port)
	authUser := strings.TrimSpace(cfg.AdminSMTPUser)
	if authUser == "" {
		authUser = cfg.AdminEmail
	}
	// Manche Server (z. B. kasserver) erwarten Identity = Username (beides E-Mail/Login).
	auth := smtp.PlainAuth(authUser, authUser, cfg.AdminSMTPPassword, cfg.AdminSMTPServer)
	msg := []byte("To: " + cfg.AdminEmail + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body + "\r\n")

	tlsMode := strings.ToLower(strings.TrimSpace(cfg.AdminSMTPTLS))
	if tlsMode == "" {
		if port == 465 {
			tlsMode = "tls"
		} else if port == 587 {
			tlsMode = "starttls"
		}
	}

	switch tlsMode {
	case "tls":
		return sendTLS(cfg, addr, auth, msg)
	case "starttls":
		return sendSTARTTLS(cfg, addr, auth, msg)
	default:
		return smtp.SendMail(addr, auth, cfg.AdminEmail, []string{cfg.AdminEmail}, msg)
	}
}

// sendTLS: implizites TLS (Port 465).
func sendTLS(cfg *config.Config, addr string, auth smtp.Auth, msg []byte) error {
	tlsConfig := &tls.Config{ServerName: cfg.AdminSMTPServer}
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.AdminSMTPServer)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(cfg.AdminEmail); err != nil {
		return err
	}
	if err := client.Rcpt(cfg.AdminEmail); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// sendSTARTTLS: Verbindung, dann STARTTLS (typisch Port 587).
func sendSTARTTLS(cfg *config.Config, addr string, auth smtp.Auth, msg []byte) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.AdminSMTPServer)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: cfg.AdminSMTPServer}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(cfg.AdminEmail); err != nil {
		return err
	}
	if err := client.Rcpt(cfg.AdminEmail); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// FormatErrorBody builds a plain-text body for error notification (subject + log excerpt).
func FormatErrorBody(subject, errDetail, logExcerpt string) string {
	var b strings.Builder
	b.WriteString(subject)
	b.WriteString("\n\n")
	b.WriteString("Fehlerdetails / Error details:\n")
	b.WriteString(errDetail)
	b.WriteString("\n\n")
	if logExcerpt != "" {
		b.WriteString("Log-Auszug / Log excerpt:\n")
		b.WriteString(logExcerpt)
	}
	return b.String()
}
