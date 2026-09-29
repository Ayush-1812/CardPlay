package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// Exercises real PostgreSQL authorization and transaction behavior. CI supplies
// an isolated TEST_DATABASE_URL; local runs skip unless explicitly configured.
func TestRoomAndChatAuthorization(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	suffix := httpx.Token()[:12]
	create := func(handle string) (string, string) {
		user, err := q.CreateUser(ctx, store.CreateUserParams{Email: handle + suffix + "@test.invalid", Handle: handle + suffix, DisplayName: handle, PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: true})
		if err != nil {
			t.Fatal(err)
		}
		raw := httpx.Token()
		if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(raw), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return user.ID, raw
	}
	alice, aliceToken := create("alice")
	bob, bobToken := create("bob")
	eve, eveToken := create("eve")
	c, err := config.Parse(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return dsn
		case "APP_ORIGIN":
			return "http://localhost:3000"
		case "APP_ENV":
			return "test"
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
	do := func(method, path, token string, body any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, srv.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if method != "GET" {
			req.Header.Set("Origin", c.Origin)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: token})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if resp.StatusCode != 204 {
			if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, out
	}
	if status, _ := do("POST", "/api/v1/rooms", "", map[string]any{"name": "private", "capacity": 2}); status != 401 {
		t.Fatalf("unauthenticated create=%d", status)
	}
	status, created := do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "private", "capacity": 2})
	if status != 201 {
		t.Fatalf("create=%d %v", status, created)
	}
	room := created["id"].(string)
	if status, _ := do("GET", "/api/v1/rooms/"+room, eveToken, nil); status != 404 {
		t.Fatalf("nonmember room read=%d", status)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+room, bobToken, nil); status != 404 {
		t.Fatalf("uninvited room read=%d", status)
	}
	status, inv := do("POST", "/api/v1/rooms/"+room+"/invitations", aliceToken, map[string]any{})
	if status != 201 {
		t.Fatalf("invite=%d %v", status, inv)
	}
	token := inv["token"].(string)
	if token == "" {
		t.Fatal("empty link token")
	}
	status, joined := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": token})
	if status != 200 {
		t.Fatalf("join=%d %v", status, joined)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", eveToken, map[string]any{"token": token}); status != 409 {
		t.Fatalf("third seat=%d", status)
	}
	status, view := do("GET", "/api/v1/rooms/"+room, bobToken, nil)
	if status != 200 {
		t.Fatalf("member read=%d %v", status, view)
	}
	members := view["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members=%d", len(members))
	}
	clientID := fmt.Sprintf("%s-%s-%s-%s-%s", suffix[:8], suffix[:4], suffix[:4], suffix[:4], strings.Repeat("a", 12))
	message := map[string]any{"body": "Hello from Bob", "client_id": clientID}
	status, first := do("POST", "/api/v1/rooms/"+room+"/chat", bobToken, message)
	if status != 200 {
		t.Fatalf("chat=%d %v", status, first)
	}
	status, second := do("POST", "/api/v1/rooms/"+room+"/chat", bobToken, message)
	if status != 200 || first["id"] != second["id"] {
		t.Fatalf("idempotent retry=%d %v", status, second)
	}
	message["body"] = "different"
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/chat", bobToken, message); status != 409 {
		t.Fatalf("changed retry=%d", status)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+room+"/chat", eveToken, nil); status != 404 {
		t.Fatalf("nonmember chat=%d", status)
	}
	status, list := do("GET", "/api/v1/rooms/"+room+"/chat", aliceToken, nil)
	if status != 200 || len(list["items"].([]any)) != 1 {
		t.Fatalf("chat page=%d %v", status, list)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO room_chat(room_id,user_id,client_id,body) SELECT $1,$2,gen_random_uuid(),'bulk '||n FROM generate_series(1,120) n ORDER BY n", room, alice); err != nil {
		t.Fatal(err)
	}
	status, list = do("GET", "/api/v1/rooms/"+room+"/chat", aliceToken, nil)
	latest := list["items"].([]any)
	if status != 200 || len(latest) != 100 || latest[0].(map[string]any)["body"] != "bulk 21" || latest[99].(map[string]any)["body"] != "bulk 120" {
		t.Fatalf("latest chat page=%d %d", status, len(latest))
	}
	status, list = do("GET", fmt.Sprintf("/api/v1/rooms/%s/chat?after=%.0f", room, first["id"]), aliceToken, nil)
	older := list["items"].([]any)
	if status != 200 || len(older) != 100 || older[0].(map[string]any)["body"] != "bulk 1" {
		t.Fatalf("chat cursor page=%d %d", status, len(older))
	}
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/matches", eveToken, nil); status != 404 {
		t.Fatalf("nonmember start=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/matches", bobToken, nil); status != 403 {
		t.Fatalf("nonhost start=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/matches", aliceToken, nil); status != 409 { // players not ready
		t.Fatalf("start before ready=%d", status)
	}
	status, found := do("GET", "/api/v1/users?handle=bob"+suffix, aliceToken, nil)
	if status != 200 || found["id"] != bob || found["email"] != nil || found["password_hash"] != nil {
		t.Fatalf("public search leaked data: %d %v", status, found)
	}
	unverified, err := q.CreateUser(ctx, store.CreateUserParams{Email: "hidden" + suffix + "@test.invalid", Handle: "hidden" + suffix, DisplayName: "Private", PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: false})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := do("GET", "/api/v1/users?handle=hidden"+suffix, aliceToken, nil); status != 404 {
		t.Fatalf("unverified search=%d", status)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+unverified.ID+"/request", aliceToken, nil); status != 204 {
		t.Fatalf("private friend request=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "Invalid", "capacity": 6}); status != 400 {
		t.Fatalf("six-player room=%d", status)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+bob+"/request", aliceToken, nil); status != 204 {
		t.Fatalf("friend request=%d", status)
	}
	if status, crossed := do("POST", "/api/v1/friendships/"+alice+"/request", bobToken, nil); status != 200 || crossed["status"] != "accepted" {
		t.Fatalf("crossed request=%d %v", status, crossed)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+alice+"/accept", bobToken, nil); status != 409 {
		t.Fatalf("accept after crossed request=%d", status)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+bob+"/request", eveToken, nil); status != 204 {
		t.Fatalf("decline setup=%d", status)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+eve+"/decline", bobToken, nil); status != 204 {
		t.Fatalf("friend decline=%d", status)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+eve+"/block", aliceToken, nil); status != 204 {
		t.Fatalf("block=%d", status)
	}
	if status, _ := do("GET", "/api/v1/users?handle=alice"+suffix, eveToken, nil); status != 404 {
		t.Fatalf("blocked search=%d", status)
	}
	status, blocks := do("GET", "/api/v1/blocks", aliceToken, nil)
	if status != 200 || len(blocks["items"].([]any)) != 1 || blocks["items"].([]any)[0].(map[string]any)["email"] != nil {
		t.Fatalf("blocks leaked data: %d %v", status, blocks)
	}
	status, created = do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "Host room", "capacity": 3})
	if status != 201 {
		t.Fatalf("host room=%d %v", status, created)
	}
	hostRoom := created["id"].(string)
	status, linkInvite := do("POST", "/api/v1/rooms/"+hostRoom+"/invitations", aliceToken, map[string]any{})
	if status != 201 {
		t.Fatalf("host link=%d %v", status, linkInvite)
	}
	status, revokedLink := do("POST", "/api/v1/rooms/"+hostRoom+"/invitations", aliceToken, map[string]any{})
	if status != 201 {
		t.Fatalf("revoked link setup=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/invitations/"+revokedLink["id"].(string), aliceToken, nil); status != 204 {
		t.Fatalf("revoke invitation=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": revokedLink["token"]}); status != 404 {
		t.Fatalf("revoked link join=%d", status)
	}
	status, personal := do("POST", "/api/v1/rooms/"+hostRoom+"/invitations", aliceToken, map[string]any{"target_id": bob})
	if status != 201 {
		t.Fatalf("friend invite=%d %v", status, personal)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", eveToken, map[string]any{"invitation_id": personal["id"]}); status != 404 {
		t.Fatalf("wrong invite target=%d", status)
	}
	status, createdInvites := do("GET", "/api/v1/rooms/"+hostRoom+"/invitations", aliceToken, nil)
	if status != 200 || len(createdInvites["items"].([]any)) != 3 || createdInvites["items"].([]any)[0].(map[string]any)["token_hash"] != nil {
		t.Fatalf("invite list leaked token: %d %v", status, createdInvites)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+hostRoom+"/invitations", eveToken, nil); status != 404 {
		t.Fatalf("outsider invite list=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"invitation_id": personal["id"]}); status != 200 {
		t.Fatalf("friend join=%d", status)
	}
	status, bobLink := do("POST", "/api/v1/rooms/"+hostRoom+"/invitations", bobToken, map[string]any{})
	if status != 201 {
		t.Fatalf("member link invite=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/invitations/"+bobLink["id"].(string), aliceToken, nil); status != 204 {
		t.Fatalf("host revokes member invite=%d", status)
	}
	if status, _ := do("PATCH", "/api/v1/rooms/"+hostRoom, bobToken, map[string]any{"name": "Hijacked", "capacity": 2}); status != 404 {
		t.Fatalf("nonhost room edit=%d", status)
	}
	if status, _ := do("PATCH", "/api/v1/rooms/"+hostRoom, aliceToken, map[string]any{"name": "Renamed", "capacity": 2}); status != 200 {
		t.Fatalf("host room edit=%d", status)
	}
	if status, _ := do("PATCH", "/api/v1/rooms/"+hostRoom, aliceToken, map[string]any{"name": "Too small", "capacity": 1}); status != 400 {
		t.Fatalf("invalid capacity=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/rooms/"+hostRoom+"/members/"+bob, bobToken, nil); status != 404 {
		t.Fatalf("nonhost kick=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/rooms/"+hostRoom+"/members/"+bob, aliceToken, nil); status != 204 {
		t.Fatalf("host kick=%d", status)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+hostRoom, bobToken, nil); status != 404 {
		t.Fatalf("kicked read=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": linkInvite["token"]}); status != 403 {
		t.Fatalf("kicked rejoin=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/rooms/"+hostRoom, bobToken, nil); status != 404 {
		t.Fatalf("nonhost close=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/rooms/"+hostRoom, aliceToken, nil); status != 204 {
		t.Fatalf("host close=%d", status)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+hostRoom, aliceToken, nil); status != 404 {
		t.Fatalf("closed room still readable=%d", status)
	}
	status, created = do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "Transfer room", "capacity": 3})
	if status != 201 {
		t.Fatalf("transfer room=%d", status)
	}
	transferRoom := created["id"].(string)
	status, linkInvite = do("POST", "/api/v1/rooms/"+transferRoom+"/invitations", aliceToken, map[string]any{})
	if status != 201 {
		t.Fatalf("transfer invite=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": linkInvite["token"]}); status != 200 {
		t.Fatalf("transfer join=%d", status)
	}
	status, leaverLink := do("POST", "/api/v1/rooms/"+transferRoom+"/invitations", bobToken, map[string]any{})
	if status != 201 {
		t.Fatalf("leaver link=%d", status)
	}
	if status, _ := do("PUT", "/api/v1/rooms/"+transferRoom+"/host", aliceToken, map[string]any{"user_id": bob}); status != 204 {
		t.Fatalf("host transfer=%d", status)
	}
	if status, _ := do("DELETE", "/api/v1/rooms/"+transferRoom, aliceToken, nil); status != 404 {
		t.Fatalf("former host close=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/"+transferRoom+"/leave", bobToken, nil); status != 204 {
		t.Fatalf("host leave=%d", status)
	}
	status, transferredView := do("GET", "/api/v1/rooms/"+transferRoom, aliceToken, nil)
	if status != 200 || transferredView["room"].(map[string]any)["host_id"] != alice {
		t.Fatalf("automatic host transfer=%d %v", status, transferredView)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", eveToken, map[string]any{"token": leaverLink["token"]}); status != 404 {
		t.Fatalf("departed member link still valid=%d", status)
	}
	status, created = do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "Absent host", "capacity": 2})
	if status != 201 {
		t.Fatalf("absent-host room=%d", status)
	}
	absentRoom := created["id"].(string)
	status, linkInvite = do("POST", "/api/v1/rooms/"+absentRoom+"/invitations", aliceToken, map[string]any{})
	if status != 201 {
		t.Fatalf("absent-host invite=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": linkInvite["token"]}); status != 200 {
		t.Fatalf("absent-host join=%d", status)
	}
	if _, err = pool.Exec(ctx, "UPDATE room_members SET last_seen_at=now()-interval '70 seconds' WHERE room_id=$1", absentRoom); err != nil {
		t.Fatal(err)
	}
	if err = app.Hub.Rooms.TransferAbsentHosts(ctx); err != nil {
		t.Fatal(err)
	}
	status, transferredView = do("GET", "/api/v1/rooms/"+absentRoom, bobToken, nil)
	if status != 200 || transferredView["room"].(map[string]any)["host_id"] != alice {
		t.Fatalf("all-offline room transferred=%d %v", status, transferredView)
	}
	if _, err = pool.Exec(ctx, "UPDATE room_members SET last_seen_at=now() WHERE room_id=$1 AND user_id=$2", absentRoom, bob); err != nil {
		t.Fatal(err)
	}
	if err = app.Hub.Rooms.TransferAbsentHosts(ctx); err != nil {
		t.Fatal(err)
	}
	status, transferredView = do("GET", "/api/v1/rooms/"+absentRoom, bobToken, nil)
	if status != 200 || transferredView["room"].(map[string]any)["host_id"] != bob {
		t.Fatalf("absent-host transfer=%d %v", status, transferredView)
	}
	status, created = do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "Block invite", "capacity": 2})
	if status != 201 {
		t.Fatalf("block-invite room=%d", status)
	}
	blockRoom := created["id"].(string)
	status, personal = do("POST", "/api/v1/rooms/"+blockRoom+"/invitations", aliceToken, map[string]any{"target_id": bob})
	if status != 201 {
		t.Fatalf("block-invite setup=%d", status)
	}
	if status, _ := do("POST", "/api/v1/friendships/"+bob+"/block", aliceToken, nil); status != 204 {
		t.Fatalf("block friend=%d", status)
	}
	status, personalList := do("GET", "/api/v1/invitations", bobToken, nil)
	if status != 200 || len(personalList["items"].([]any)) != 0 {
		t.Fatalf("blocked invitation visible=%d %v", status, personalList)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"invitation_id": personal["id"]}); status != 404 {
		t.Fatalf("blocked invitation accepted=%d", status)
	}
	if status, _ := do("PATCH", "/api/v1/me", aliceToken, map[string]any{"display_name": "Alice New"}); status != 200 {
		t.Fatalf("profile update=%d", status)
	}
	status, sessions := do("GET", "/api/v1/sessions", bobToken, nil)
	if status != 200 || len(sessions["items"].([]any)) != 1 {
		t.Fatalf("session list=%d %v", status, sessions)
	}
	if status, _ := do("DELETE", "/api/v1/sessions/"+sessions["items"].([]any)[0].(map[string]any)["id"].(string), bobToken, nil); status != 204 {
		t.Fatalf("session revocation=%d", status)
	}
	if status, _ := do("GET", "/api/v1/me", bobToken, nil); status != 401 {
		t.Fatalf("revoked session still valid=%d", status)
	}
	resetToken := httpx.Token()
	if err = q.CreateAccountToken(ctx, store.CreateAccountTokenParams{TokenHash: httpx.Hash(resetToken), UserID: alice, Purpose: "reset_password", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if status, _ := do("POST", "/api/v1/auth/password/reset", "", map[string]any{"token": resetToken, "password": "new-test-password-456"}); status != 200 {
		t.Fatalf("password reset=%d", status)
	}
	if status, _ := do("GET", "/api/v1/me", aliceToken, nil); status != 401 {
		t.Fatalf("old session after reset=%d", status)
	}
	if status, _ := do("POST", "/api/v1/auth/login", "", map[string]any{"email": "alice" + suffix + "@test.invalid", "password": "test-password-123"}); status != 401 {
		t.Fatalf("old password after reset=%d", status)
	}
	if status, _ := do("POST", "/api/v1/auth/login", "", map[string]any{"email": "alice" + suffix + "@test.invalid", "password": "new-test-password-456"}); status != 200 {
		t.Fatalf("new password after reset=%d", status)
	}
	newSession := httpx.Token()
	if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(newSession), UserID: alice, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if status, _ := do("DELETE", "/api/v1/me", newSession, map[string]any{"password": "new-test-password-456"}); status != 204 {
		t.Fatalf("account deletion=%d", status)
	}
	if status, _ := do("GET", "/api/v1/me", newSession, nil); status != 401 {
		t.Fatalf("deleted account session=%d", status)
	}
	bobAgain := httpx.Token()
	if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(bobAgain), UserID: bob, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	status, inherited := do("GET", "/api/v1/rooms/"+room, bobAgain, nil)
	if status != 200 || inherited["room"].(map[string]any)["host_id"] != bob || len(inherited["members"].([]any)) != 1 {
		t.Fatalf("deleted host room not handed on=%d %v", status, inherited)
	}
	unverifiedReset := httpx.Token()
	if err = q.CreateAccountToken(ctx, store.CreateAccountTokenParams{TokenHash: httpx.Hash(unverifiedReset), UserID: unverified.ID, Purpose: "reset_password", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if status, _ := do("POST", "/api/v1/auth/password/reset", "", map[string]any{"token": unverifiedReset, "password": "verified-by-reset-1"}); status != 200 {
		t.Fatalf("unverified reset=%d", status)
	}
	if reset, err := q.UserByEmail(ctx, unverified.Email); err != nil || !reset.EmailVerified {
		t.Fatalf("reset did not verify email: %v", err)
	}
	_ = alice
	_ = bob
}
