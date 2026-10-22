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
	// MigrateOnStart lets a single-process host apply embedded migrations before
	// serving traffic when it cannot run a separate pre-deploy command.
	MigrateOnStart bool
	// MetricsAddress serves /metrics on a separate listener when set. Bind it
	// to a private interface: metrics are not authenticated.
	MetricsAddress string
	// DBMaxConns bounds the PostgreSQL pool per API instance.
	DBMaxConns int32
	// TurnTimeout is how long a match waits for the awaited player before the
	// server plays the default move for them; 0 disables it.
	TurnTimeout time.Duration
	// Voice configures in-game voice chat. Disabled voice leaves the rest of
	// the room untouched: signalling is refused and the client hides it.
	Voice Voice
}

// Voice is the WebRTC configuration players need to reach each other. The
// TURN secret never leaves the server; each player is issued a short-lived
// credential derived from it (coturn's REST scheme).
type Voice struct {
	Enabled    bool
	STUNURLs   []string
	TURNURLs   []string
	TURNSecret string
	TURNTTL    time.Duration
}

func Load() (Config, error) { return Parse(os.Getenv) }

// splitList reads a comma-separated setting, ignoring empty entries and
// surrounding spaces.
func splitList(v string) []string {
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func Parse(get func(string) string) (Config, error) {
	c := Config{Environment: get("APP_ENV"), Address: get("HTTP_ADDR"), DatabaseURL: get("DATABASE_URL"), Origin: get("APP_ORIGIN"), SMTPAddress: get("SMTP_ADDR"), MailFrom: get("MAIL_FROM"), SMTPUsername: get("SMTP_USERNAME"), SMTPPassword: get("SMTP_PASSWORD"), SessionTTL: 7 * 24 * time.Hour, MetricsAddress: get("METRICS_ADDR"), DBMaxConns: 20, TurnTimeout: 2 * time.Minute}
	if c.Environment == "" {
		c.Environment = "development"
	}
	if c.Address == "" {
		c.Address = ":8080"
	}
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return c, errors.New("APP_ENV must be development, test or production")
	}
	switch get("MIGRATE_ON_START") {
	case "", "0":
	case "1":
		c.MigrateOnStart = true
	default:
		return c, errors.New("MIGRATE_ON_START must be 0 or 1")
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
	if v := get("MATCH_TURN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || (d != 0 && (d < 15*time.Second || d > time.Hour)) {
			return c, errors.New("MATCH_TURN_TIMEOUT must be 0 (off) or between 15s and 1h, for example 2m")
		}
		c.TurnTimeout = d
	}
	c.Voice = Voice{Enabled: true, TURNSecret: get("VOICE_TURN_SECRET"), TURNTTL: 10 * time.Minute, STUNURLs: []string{"stun:stun.l.google.com:19302"}}
	switch get("VOICE_ENABLED") {
	case "", "1":
	case "0":
		c.Voice.Enabled = false
	default:
		return c, errors.New("VOICE_ENABLED must be 0 or 1")
	}
	if v := get("VOICE_STUN_URLS"); v != "" {
		c.Voice.STUNURLs = splitList(v)
	}
	c.Voice.TURNURLs = splitList(get("VOICE_TURN_URLS"))
	for _, raw := range c.Voice.STUNURLs {
		if !strings.HasPrefix(raw, "stun:") && !strings.HasPrefix(raw, "stuns:") {
			return c, errors.New("VOICE_STUN_URLS entries must start with stun: or stuns:")
		}
	}
	for _, raw := range c.Voice.TURNURLs {
		if !strings.HasPrefix(raw, "turn:") && !strings.HasPrefix(raw, "turns:") {
			return c, errors.New("VOICE_TURN_URLS entries must start with turn: or turns:")
		}
	}
	if len(c.Voice.TURNURLs) > 0 && len(c.Voice.TURNSecret) < 16 {
		return c, errors.New("VOICE_TURN_URLS needs VOICE_TURN_SECRET of at least 16 characters")
	}
	if v := get("VOICE_TURN_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > time.Hour {
			return c, errors.New("VOICE_TURN_TTL must be between 1m and 1h, for example 10m")
		}
		c.Voice.TURNTTL = d
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
