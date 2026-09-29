package accounts

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"time"

	"cardplay/internal/config"
	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

const CookieName = "cardplay_session"

type Module struct {
	DB     *pgxpool.Pool
	Config config.Config
}

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

func PasswordHash(password string) string {
	salt := httpx.Token()
	b := argon2.IDKey([]byte(password), []byte(salt), 2, 64*1024, 2, 32)
	return salt + ":" + base64.RawStdEncoding.EncodeToString(b)
}
func PasswordMatches(encoded, password string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	got := argon2.IDKey([]byte(password), []byte(parts[0]), 2, 64*1024, 2, 32)
	want, err := base64.RawStdEncoding.DecodeString(parts[1])
	return err == nil && subtle.ConstantTimeCompare(want, got) == 1
}

func (m *Module) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(CookieName); err == nil && len(c.Value) == 64 {
			hash := httpx.Hash(c.Value)
			u, err := store.New(m.DB).SessionUser(r.Context(), hash)
			if err == nil {
				r = r.WithContext(httpx.WithIdentity(r.Context(), httpx.Identity{ID: u.ID, Handle: u.Handle, DisplayName: u.DisplayName, Email: u.Email, Verified: u.EmailVerified, SessionHash: hash}))
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (m *Module) Register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Handle = strings.ToLower(strings.TrimSpace(in.Handle))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email || len(in.Email) > 254 || !handlePattern.MatchString(in.Handle) || len([]rune(in.DisplayName)) < 1 || len([]rune(in.DisplayName)) > 60 || len(in.Password) < 12 || len(in.Password) > 128 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Use a valid email, 3-24 character handle, display name and 12-128 byte password")
		return
	}
	if m.Config.SMTPAddress == "" {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Email delivery is not configured; use seeded accounts in local development")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	user, err := q.CreateUser(r.Context(), store.CreateUserParams{Email: in.Email, Handle: in.Handle, DisplayName: in.DisplayName, PasswordHash: PasswordHash(in.Password)})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	token := httpx.Token()
	err = q.CreateAccountToken(r.Context(), store.CreateAccountTokenParams{TokenHash: httpx.Hash(token), UserID: user.ID, Purpose: "verify_email", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	// Delivery occurs before commit: failure leaves no unusable account. The user
	// confirms manually after receiving the token; registration never auto-verifies.
	body := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: Verify your CardPlay account\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOpen %s and enter this verification token:\r\n%s\r\nIt expires in one hour.\r\n", in.Email, m.Config.MailFrom, m.Config.Origin, token)
	if err = sendMail(r.Context(), m.Config.SMTPAddress, m.Config.MailFrom, in.Email, body); err != nil {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Verification mail could not be delivered; try again")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": user.ID, "verification_required": true})
}
func sendMail(ctx context.Context, address, from, to, body string) error {
	// Local Mailpit / trusted SMTP relay. Timeout enforced by the socket.
	return sendSMTP(ctx, address, from, to, body)
}
func (m *Module) Verify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	id, err := q.ConsumeAccountToken(r.Context(), store.ConsumeAccountTokenParams{TokenHash: httpx.Hash(in.Token), Purpose: "verify_email"})
	if err != nil {
		httpx.Error(w, r, 400, "TOKEN_INVALID", "Token is invalid or expired")
		return
	}
	if err = q.VerifyUser(r.Context(), id); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]bool{"verified": true})
}
func (m *Module) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if len(in.Password) > 128 {
		httpx.Error(w, r, 401, "INVALID_CREDENTIALS", "Invalid email or password")
		return
	}
	q := store.New(m.DB)
	u, err := q.UserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
	hash := u.PasswordHash
	if err != nil {
		hash = strings.Repeat("0", 64) + ":" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	}
	if !PasswordMatches(hash, in.Password) || err != nil {
		httpx.Error(w, r, 401, "INVALID_CREDENTIALS", "Invalid email or password")
		return
	}
	token := httpx.Token()
	expires := time.Now().Add(m.Config.SessionTTL)
	if err = q.CreateSession(r.Context(), store.CreateSessionParams{TokenHash: httpx.Hash(token), UserID: u.ID, ExpiresAt: expires}); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: m.Config.SecureCookie, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(m.Config.SessionTTL.Seconds())})
	httpx.JSON(w, 200, map[string]any{"id": u.ID, "handle": u.Handle, "display_name": u.DisplayName, "email_verified": u.EmailVerified})
}
func (m *Module) Logout(w http.ResponseWriter, r *http.Request) {
	if err := store.New(m.DB).DeleteSession(r.Context(), httpx.Actor(r).SessionHash); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.Config.SecureCookie, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}
func (m *Module) Me(w http.ResponseWriter, r *http.Request) {
	a := httpx.Actor(r)
	httpx.JSON(w, 200, map[string]any{"id": a.ID, "handle": a.Handle, "display_name": a.DisplayName, "email": a.Email, "email_verified": a.Verified})
}

// Compile-time marker ensures SMTP remains an explicit delivery adapter.
var _ *smtp.Client
