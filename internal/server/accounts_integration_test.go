package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/httpx"
	"cardplay/internal/server"
	"cardplay/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testSMTP(t *testing.T) (string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	messages := make(chan string, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				_, _ = fmt.Fprint(conn, "220 test ESMTP\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = fmt.Fprint(conn, "250-test\r\n250 8BITMIME\r\n")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						_, _ = fmt.Fprint(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						_, _ = fmt.Fprint(conn, "354 Go ahead\r\n")
						var body strings.Builder
						for {
							part, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							if part == ".\r\n" {
								break
							}
							body.WriteString(part)
						}
						messages <- body.String()
						_, _ = fmt.Fprint(conn, "250 Stored\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = fmt.Fprint(conn, "221 Bye\r\n")
						return
					default:
						_, _ = fmt.Fprint(conn, "500 Unsupported\r\n")
					}
				}
			}()
		}
	}()
	return ln.Addr().String(), messages
}

func TestAccountEmailAndSessionFlows(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	smtpAddress, messages := testSMTP(t)
	c, err := config.Parse(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return dsn
		case "APP_ORIGIN":
			return "http://localhost:3000"
		case "APP_ENV":
			return "test"
		case "SMTP_ADDR":
			return smtpAddress
		case "MAIL_FROM":
			return "hello@cardplay.test"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	app := server.New(pool, c)
	srv := httptest.NewServer(app.Handler)
	defer srv.Close()
	go app.Hub.Run(ctx)
	request := func(path, cookie string, body any) (int, map[string]any, string) {
		t.Helper()
		data, _ := json.Marshal(body)
		req, err := http.NewRequest("POST", srv.URL+"/api/v1"+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", c.Origin)
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		if resp.StatusCode != 204 && resp.StatusCode != 202 {
			if err = json.NewDecoder(resp.Body).Decode(&result); err != nil && err != io.EOF {
				t.Fatal(err)
			}
		}
		var token string
		for _, item := range resp.Cookies() {
			if item.Name == accounts.CookieName {
				token = item.Value
			}
		}
		return resp.StatusCode, result, token
	}
	getMe := func(cookie string) int {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/me", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	suffix := httpx.Token()[:12]
	email, oldPassword := "flow"+suffix+"@test.invalid", "old-password-123"
	status, result, _ := request("/auth/register", "", map[string]any{"email": email, "handle": "flow" + suffix, "display_name": "Flow Player", "password": oldPassword})
	if status != 201 || result["email"] != nil || result["password_hash"] != nil {
		t.Fatalf("register=%d %v", status, result)
	}
	readToken := func() string {
		t.Helper()
		select {
		case message := <-messages:
			token := regexp.MustCompile(`[0-9a-f]{64}`).FindString(message)
			if token == "" {
				t.Fatal("mail omitted token")
			}
			return token
		case <-time.After(3 * time.Second):
			t.Fatal("mail was not sent")
		}
		return ""
	}
	first := readToken()
	if status, _, _ = request("/auth/verify/resend", "", map[string]any{"email": email}); status != 202 {
		t.Fatalf("resend=%d", status)
	}
	second := readToken()
	if status, _, _ = request("/auth/verify", "", map[string]any{"token": first}); status != 400 {
		t.Fatalf("old verify token=%d", status)
	}
	if status, _, _ = request("/auth/verify", "", map[string]any{"token": second}); status != 200 {
		t.Fatalf("verify=%d", status)
	}
	status, again, _ := request("/auth/register", "", map[string]any{"email": email, "handle": "other" + suffix, "display_name": "Again", "password": "another-password-1"})
	if status != 201 || fmt.Sprint(again) != fmt.Sprint(result) {
		t.Fatalf("existing email register=%d %v, want %v", status, again, result)
	}
	select {
	case message := <-messages:
		if !strings.Contains(message, "already has one") {
			t.Fatal("existing-account notice missing")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("existing-account notice was not sent")
	}
	if status, _, _ = request("/auth/register", "", map[string]any{"email": "taken" + suffix + "@test.invalid", "handle": "flow" + suffix, "display_name": "Taken", "password": "another-password-1"}); status != 409 {
		t.Fatalf("taken handle=%d", status)
	}
	if status, _, _ = request("/auth/password/forgot", "", map[string]any{"email": "missing" + suffix + "@test.invalid"}); status != 202 {
		t.Fatalf("unknown recovery=%d", status)
	}
	if status, _, _ = request("/auth/password/forgot", "", map[string]any{"email": email}); status != 202 {
		t.Fatalf("recovery=%d", status)
	}
	resetToken := readToken()
	status, _, session := request("/auth/login", "", map[string]any{"email": email, "password": oldPassword})
	if status != 200 || len(session) != 64 || getMe(session) != 200 {
		t.Fatalf("login or session=%d", status)
	}
	if _, err = store.New(pool).SessionUser(ctx, httpx.Hash(session)); err != nil {
		t.Fatalf("hashed session not stored: %v", err)
	}
	if status, _, _ = request("/auth/password/reset", "", map[string]any{"token": resetToken, "password": "reset-password-456"}); status != 200 {
		t.Fatalf("reset=%d", status)
	}
	if getMe(session) != 401 {
		t.Fatal("old session survived reset")
	}
	if status, _, _ = request("/auth/password/reset", "", map[string]any{"token": resetToken, "password": "another-password-789"}); status != 400 {
		t.Fatalf("reused reset token=%d", status)
	}
	status, _, session = request("/auth/login", "", map[string]any{"email": email, "password": "reset-password-456"})
	if status != 200 {
		t.Fatalf("new login=%d", status)
	}
	changeBody, _ := json.Marshal(map[string]string{"current_password": "reset-password-456", "new_password": "changed-password-789"})
	changeReq, _ := http.NewRequest("PUT", srv.URL+"/api/v1/me/password", bytes.NewReader(changeBody))
	changeReq.Header.Set("Origin", c.Origin)
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: session})
	changeResp, err := http.DefaultClient.Do(changeReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = changeResp.Body.Close()
	if changeResp.StatusCode != 204 || getMe(session) != 401 {
		t.Fatalf("change password or session revocation=%d", changeResp.StatusCode)
	}
	if status, _, _ = request("/auth/login", "", map[string]any{"email": email, "password": "reset-password-456"}); status != 401 {
		t.Fatalf("old changed password=%d", status)
	}
	status, _, session = request("/auth/login", "", map[string]any{"email": email, "password": "changed-password-789"})
	if status != 200 {
		t.Fatalf("changed password login=%d", status)
	}
	if status, _, _ = request("/auth/logout", session, map[string]any{}); status != 204 || getMe(session) != 401 {
		t.Fatalf("logout=%d", status)
	}
	for i := 0; i < 11; i++ {
		status, _, _ = request("/auth/login", "", map[string]any{"email": "rate" + suffix + "@test.invalid", "password": "wrong-password"})
	}
	if status != 429 {
		t.Fatalf("auth rate limit=%d", status)
	}
	// A stranger's failures must not lock the owner out of a known browser,
	// and an invented device cookie must not escape the shared limit.
	login := func(device, password string) (int, string) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"email": email, "password": password})
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/auth/login", bytes.NewReader(data))
		req.Header.Set("Origin", c.Origin)
		req.Header.Set("Content-Type", "application/json")
		if device != "" {
			req.AddCookie(&http.Cookie{Name: accounts.DeviceCookieName, Value: device})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		for _, item := range resp.Cookies() {
			if item.Name == accounts.DeviceCookieName {
				return resp.StatusCode, item.Value
			}
		}
		return resp.StatusCode, ""
	}
	status, device := login("", "changed-password-789")
	if status != 200 || len(device) != 64 {
		t.Fatalf("device login=%d cookie=%q", status, device)
	}
	for i := 0; i < 11; i++ {
		status, _ = login("", "wrong-password")
	}
	if status != 429 {
		t.Fatalf("stranger lockout attempts=%d", status)
	}
	if status, _ = login(httpx.Token(), "changed-password-789"); status != 429 {
		t.Fatalf("invented device cookie bypassed limit=%d", status)
	}
	if status, _ = login(device, "changed-password-789"); status != 200 {
		t.Fatalf("known device locked out=%d", status)
	}
}
