package config

import "testing"

func TestProductionRejectsUnsafeOriginAndDatabase(t *testing.T) {
	values := map[string]string{"APP_ENV": "production", "APP_ORIGIN": "http://localhost:3000", "DATABASE_URL": "postgres://user:password@localhost/db?sslmode=disable", "SMTP_ADDR": "localhost:25", "MAIL_FROM": "hello@example.com", "SMTP_USERNAME": "user", "SMTP_PASSWORD": "secret"}
	get := func(k string) string { return values[k] }
	if _, err := Parse(get); err == nil {
		t.Fatal("accepted insecure origin")
	}
	values["APP_ORIGIN"] = "https://example.com"
	if _, err := Parse(get); err == nil {
		t.Fatal("accepted insecure PostgreSQL TLS mode")
	}
	values["DATABASE_URL"] = "postgres://user:password@db.example.com/db?sslmode=verify-full"
	if _, err := Parse(get); err != nil {
		t.Fatal(err)
	}
}
