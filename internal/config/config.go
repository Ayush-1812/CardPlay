package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment, Address, DatabaseURL, Origin, SMTPAddress, MailFrom, SMTPUsername, SMTPPassword string
	SessionTTL                                                                                   time.Duration
	SecureCookie                                                                                 bool
	// MetricsAddress serves /metrics on a separate listener when set. Bind it
	// to a private interface: metrics are not authenticated.
	MetricsAddress string
	// DBMaxConns bounds the PostgreSQL pool per API instance.
	DBMaxConns int32
}

func Load() (Config, error) { return Parse(os.Getenv) }

func Parse(get func(string) string) (Config, error) {
	c := Config{Environment: get("APP_ENV"), Address: get("HTTP_ADDR"), DatabaseURL: get("DATABASE_URL"), Origin: get("APP_ORIGIN"), SMTPAddress: get("SMTP_ADDR"), MailFrom: get("MAIL_FROM"), SMTPUsername: get("SMTP_USERNAME"), SMTPPassword: get("SMTP_PASSWORD"), SessionTTL: 7 * 24 * time.Hour, MetricsAddress: get("METRICS_ADDR"), DBMaxConns: 20}
	if c.Environment == "" {
		c.Environment = "development"
	}
	if c.Address == "" {
		c.Address = ":8080"
	}
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return c, errors.New("APP_ENV must be development, test or production")
	}
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return c, errors.New("HTTP_ADDR must be host:port")
	}
	if c.MetricsAddress != "" {
		if _, _, err := net.SplitHostPort(c.MetricsAddress); err != nil || c.MetricsAddress == c.Address {
			return c, errors.New("METRICS_ADDR must be host:port and differ from HTTP_ADDR")
		}
	}
	if v := get("DB_MAX_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 4 || n > 200 {
			return c, errors.New("DB_MAX_CONNS must be 4-200")
		}
		c.DBMaxConns = int32(n)
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return c, errors.New("DATABASE_URL must be a PostgreSQL URL")
	}
	o, err := url.Parse(c.Origin)
	if err != nil || (o.Scheme != "http" && o.Scheme != "https") || o.Host == "" || o.User != nil || o.Path != "" || o.RawQuery != "" || o.Fragment != "" {
		return c, errors.New("APP_ORIGIN must be one exact http(s) origin without trailing slash")
	}
	c.SecureCookie = o.Scheme == "https"
	if c.SMTPAddress != "" {
		if _, _, err := net.SplitHostPort(c.SMTPAddress); err != nil {
			return c, errors.New("SMTP_ADDR must be host:port")
		}
		if (c.SMTPUsername == "") != (c.SMTPPassword == "") {
			return c, errors.New("SMTP_USERNAME and SMTP_PASSWORD must be set together")
		}
		if !strings.Contains(c.MailFrom, "@") || strings.ContainsAny(c.MailFrom, "\r\n") {
			return c, errors.New("MAIL_FROM must be an email address")
		}
	}
	if c.Environment == "production" {
		if !c.SecureCookie {
			return c, errors.New("production requires HTTPS APP_ORIGIN")
		}
		if u.Query().Get("sslmode") != "verify-full" {
			return c, errors.New("production requires DATABASE_URL sslmode=verify-full")
		}
		if c.SMTPAddress == "" || c.SMTPUsername == "" {
			return c, errors.New("production requires SMTP_ADDR and SMTP credentials")
		}
	}
	return c, nil
}
