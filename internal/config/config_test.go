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

func TestMetricsAddressAndPoolSize(t *testing.T) {
	values := map[string]string{"APP_ORIGIN": "http://localhost:3000", "DATABASE_URL": "postgres://u:p@localhost/db"}
	get := func(k string) string { return values[k] }
	c, err := Parse(get)
	if err != nil || c.MetricsAddress != "" || c.DBMaxConns != 20 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, bad := range []map[string]string{{"METRICS_ADDR": "9090"}, {"METRICS_ADDR": ":8080"}, {"DB_MAX_CONNS": "2"}, {"DB_MAX_CONNS": "many"}} {
		for k, v := range bad {
			values[k] = v
		}
		if _, err := Parse(get); err == nil {
			t.Errorf("accepted %v", bad)
		}
		delete(values, "METRICS_ADDR")
		delete(values, "DB_MAX_CONNS")
	}
	values["METRICS_ADDR"], values["DB_MAX_CONNS"] = "127.0.0.1:9090", "40"
	if c, err = Parse(get); err != nil || c.MetricsAddress != "127.0.0.1:9090" || c.DBMaxConns != 40 {
		t.Fatalf("explicit: %+v %v", c, err)
	}
}
