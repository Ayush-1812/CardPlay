package voice

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTURNCredentialsExpireAndBelongToOnePlayer checks the credential a
// player is handed: it names them, it expires, and it is derived from the
// shared secret, which never leaves the server.
func TestTURNCredentialsExpireAndBelongToOnePlayer(t *testing.T) {
	secret := "a-very-secret-value"
	m := &Module{Config: Config{
		Enabled:    true,
		STUNURLs:   []string{"stun:stun.example:3478"},
		TURNURLs:   []string{"turn:turn.example:3478?transport=udp"},
		TURNSecret: secret,
		TURNTTL:    10 * time.Minute,
	}}
	now := time.Unix(1_700_000_000, 0)
	servers, ttl := m.iceServers("player-one", now)
	if ttl != 10*time.Minute {
		t.Fatalf("ttl %s", ttl)
	}
	if len(servers) != 2 {
		t.Fatalf("got %d servers, want a STUN and a TURN entry", len(servers))
	}
	if servers[0].Username != "" || servers[0].Credential != "" {
		t.Fatal("the STUN entry must carry no credential")
	}
	turn := servers[1]
	expiry, user, ok := strings.Cut(turn.Username, ":")
	if !ok || user != "player-one" {
		t.Fatalf("username %q does not name the player", turn.Username)
	}
	at, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || at != now.Add(10*time.Minute).Unix() {
		t.Fatalf("username %q does not expire in ten minutes", turn.Username)
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(turn.Username))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); turn.Credential != want {
		t.Fatal("the credential is not derived from the shared secret")
	}
	// Another player gets another credential, so one cannot stand in for the
	// other if it leaks.
	other, _ := m.iceServers("player-two", now)
	if other[1].Credential == turn.Credential {
		t.Fatal("two players were given the same credential")
	}
	// The secret itself is never part of what is sent.
	for _, s := range servers {
		if strings.Contains(s.Username+s.Credential, secret) {
			t.Fatal("the shared secret leaked into a credential")
		}
	}
}

// TestNoTURNConfigured leaves voice working on permissive networks without
// inventing a relay that does not exist.
func TestNoTURNConfigured(t *testing.T) {
	m := &Module{Config: Config{Enabled: true, STUNURLs: []string{"stun:stun.example:3478"}}}
	servers, _ := m.iceServers("player", time.Now())
	if len(servers) != 1 || servers[0].Username != "" {
		t.Fatalf("expected a lone STUN entry, got %+v", servers)
	}
}

// TestChannelMembership covers the registry the signalling layer trusts.
func TestChannelMembership(t *testing.T) {
	m := &Module{Config: Config{Enabled: true}}
	if _, err := m.admit("room-a", "ava", "socket-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.admit("room-a", "ben", "socket-2"); err != nil {
		t.Fatal(err)
	}
	if !m.InChannel("room-a", "ava") || !m.InChannel("room-a", "ben") {
		t.Fatal("both players should be in the channel")
	}
	// A different room is a different channel: no signal may cross.
	if _, err := m.admit("room-b", "cara", "socket-3"); err != nil {
		t.Fatal(err)
	}
	if m.InChannel("room-a", "cara") || m.InChannel("room-b", "ava") {
		t.Fatal("channels must not leak across rooms")
	}

	// State is published for the others to show.
	list, ok := m.SetState("room-a", "ava", true, false, false)
	if !ok {
		t.Fatal("ava is in the channel")
	}
	if len(list) != 2 || !list[0].Muted {
		t.Fatalf("muted state was not recorded: %+v", list)
	}
	if _, ok := m.SetState("room-a", "stranger", true, false, false); ok {
		t.Fatal("a stranger's state must not be accepted")
	}

	// A second tab takes the membership over; the first tab's socket closing
	// must not remove the player the second tab put there.
	if _, err := m.admit("room-a", "ava", "socket-9"); err != nil {
		t.Fatal(err)
	}
	if left := m.Leave("room-a", "ava", "socket-1"); left != nil {
		t.Fatal("the stale socket removed a live membership")
	}
	if !m.InChannel("room-a", "ava") {
		t.Fatal("ava should still be in the channel")
	}
	if left := m.Leave("room-a", "ava", "socket-9"); len(left) != 1 {
		t.Fatalf("leaving left %+v", left)
	}
	if m.InChannel("room-a", "ava") {
		t.Fatal("ava should be gone")
	}
	// The last player out empties the room rather than leaving it behind.
	if left := m.Leave("room-a", "ben", "socket-2"); len(left) != 0 {
		t.Fatalf("the channel should be empty, got %+v", left)
	}
	if len(m.Participants("room-a")) != 0 {
		t.Fatal("an empty channel should be forgotten")
	}
}

// TestChannelIsBounded stops a mesh growing past what a phone can send.
func TestChannelIsBounded(t *testing.T) {
	m := &Module{Config: Config{Enabled: true}}
	for i := 0; i < MaxParticipants; i++ {
		if _, err := m.admit("room", "player-"+strconv.Itoa(i), "socket"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := m.admit("room", "one-too-many", "socket")
	var ve *Error
	if !errorAs(err, &ve) || ve.Code != "VOICE_FULL" {
		t.Fatalf("expected VOICE_FULL, got %v", err)
	}
	// Someone already in may still rejoin: that is a reconnection, not a
	// ninth player.
	if _, err := m.admit("room", "player-0", "socket-new"); err != nil {
		t.Fatalf("a rejoin was refused: %v", err)
	}
}

// errorAs keeps the test readable without importing errors for one call.
func errorAs(err error, target **Error) bool {
	ve, ok := err.(*Error)
	if ok {
		*target = ve
	}
	return ok
}
