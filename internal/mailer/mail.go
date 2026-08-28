package mailer

import (
	"encoding/base64"
	"fmt"
	"net/mail"
	"net/smtp"
	"os/exec"
	"strings"

	"github.com/PacificDailyTimes/pdt-news/internal/config"
)

func Send(cfg *config.Config, to, subject, body string) error {
	if cfg.MailTransport == "off" || to == "" {
		return fmt.Errorf("mail off")
	}
	from := cfg.MailFrom
	if from == "" {
		from = "noreply@localhost"
	}
	msg := "From: " + cfg.MailName + " <" + from + ">\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if cfg.MailTransport == "smtp" {
		auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
		addr := cfg.SMTPHost + ":" + cfg.SMTPPort
		return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
	}
	cmd := exec.Command("sendmail", "-t")
	cmd.Stdin = strings.NewReader(msg)
	return cmd.Run()
}

func SendPDF(cfg *config.Config, to, subject, body string, pdf []byte, name string) error {
	from := cfg.MailFrom
	bound := "pdtbound"
	var b strings.Builder
	b.WriteString("From: " + cfg.MailName + " <" + from + ">\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=" + bound + "\r\n\r\n")
	b.WriteString("--" + bound + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	b.WriteString("--" + bound + "\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"" + name + "\"\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString(pdf))
	b.WriteString("\r\n--" + bound + "--\r\n")
	if cfg.MailTransport == "smtp" {
		auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
		return smtp.SendMail(cfg.SMTPHost+":"+cfg.SMTPPort, auth, from, []string{to}, []byte(b.String()))
	}
	cmd := exec.Command("sendmail", "-t")
	cmd.Stdin = strings.NewReader(b.String())
	return cmd.Run()
}

func ValidEmail(s string) bool {
	_, err := mail.ParseAddress(s)
	return err == nil
}
