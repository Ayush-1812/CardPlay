package accounts

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"runtime"
	"strings"
	"time"

	"cardplay/internal/config"
	"cardplay/internal/httpx"
	"cardplay/internal/matches"
	"cardplay/internal/obs"
	"cardplay/internal/rooms"
	"cardplay/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

const CookieName = "cardplay_session"

// DeviceCookieName marks a browser that has signed in to an account before.
const DeviceCookieName = "cardplay_device"
const DeviceTTL = 180 * 24 * time.Hour

// KnownDevice reports whether token is an unexpired device of the account
// with this email. Lookup errors count as unknown.
func (m *Module) KnownDevice(ctx context.Context, token, email string) bool {
	if len(token) != 64 {
		return false
	}
	ok, err := store.New(m.DB).LoginDeviceKnown(ctx, store.LoginDeviceKnownParams{TokenHash: httpx.Hash(token), Email: email})
	return err == nil && ok
}

type Module struct {
	DB     *pgxpool.Pool
	Config config.Config
}

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

// Each Argon2id hash uses 64 MiB. Hashing runs at most one per CPU so a
// burst of logins queues briefly instead of exhausting memory.
var hashSlots = make(chan struct{}, max(1, runtime.NumCPU()))

func argon(password, salt []byte) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2.IDKey(password, salt, 2, 64*1024, 2, 32)
}

func PasswordHash(password string) string {
	salt := httpx.Token()
	b := argon([]byte(password), []byte(salt))
	return salt + ":" + base64.RawStdEncoding.EncodeToString(b)
}
func PasswordMatches(encoded, password string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	got := argon([]byte(password), []byte(parts[0]))
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
	displayName, nameOK := httpx.CleanText(in.DisplayName, 60)
	in.DisplayName = displayName
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email || len(in.Email) > 254 || !handlePattern.MatchString(in.Handle) || !nameOK || len(in.Password) < 12 || len(in.Password) > 128 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Use a valid email, 3-24 character handle, display name and 12-128 byte password")
		return
	}
	if m.Config.SMTPAddress == "" {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Email delivery is not configured; use seeded accounts in local development")
		return
	}
	// Hash on every path so response timing does not reveal existing accounts.
	passwordHash := PasswordHash(in.Password)
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	if _, err = q.UserByEmail(r.Context(), in.Email); err == nil {
		m.registrationExists(w, r, in.Email)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		httpx.DBError(w, r, err)
		return
	}
	user, err := q.CreateUser(r.Context(), store.CreateUserParams{Email: in.Email, Handle: in.Handle, DisplayName: in.DisplayName, PasswordHash: passwordHash})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		// Handles are public, so a taken handle may be reported. A concurrent
		// registration of the same email gets the generic response.
		if pe.ConstraintName == "users_handle_key" {
			httpx.Error(w, r, 409, "CONFLICT", "That handle is taken")
		} else {
			m.registrationExists(w, r, in.Email)
		}
		return
	}
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
	if err = m.sendMail(r.Context(), in.Email, body); err != nil {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Verification mail could not be delivered; try again")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"verification_required": true})
}

// registrationExists answers exactly like a new registration and tells the
// address owner, so registering never reveals whether an email has an account.
func (m *Module) registrationExists(w http.ResponseWriter, r *http.Request, email string) {
	body := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: Your CardPlay account\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nSomeone tried to create a CardPlay account with this email address, but it already has one.\r\nIf that was you, sign in at %s or use Forgot password there. Otherwise you can ignore this message.\r\n", email, m.Config.MailFrom, m.Config.Origin)
	if err := m.sendMail(r.Context(), email, body); err != nil {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Verification mail could not be delivered; try again")
		return
	}
	httpx.JSON(w, 201, map[string]any{"verification_required": true})
}
func (m *Module) sendMail(ctx context.Context, to, body string) error {
	return sendSMTP(ctx, m.Config.SMTPAddress, m.Config.MailFrom, to, body, m.Config.SMTPUsername, m.Config.SMTPPassword, m.Config.Environment == "production")
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
		obs.Logins.Inc("rejected")
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
	// Remember this browser so its future logins get their own rate-limit
	// bucket; failures sent by strangers cannot lock it out. It grants no access.
	device := httpx.Token()
	if err = q.CreateLoginDevice(r.Context(), store.CreateLoginDeviceParams{TokenHash: httpx.Hash(device), UserID: u.ID, ExpiresAt: time.Now().Add(DeviceTTL)}); err == nil {
		http.SetCookie(w, &http.Cookie{Name: DeviceCookieName, Value: device, Path: "/api/v1/auth/login", HttpOnly: true, Secure: m.Config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int(DeviceTTL.Seconds())})
	}
	obs.Logins.Inc("success")
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

func (m *Module) issueAccountToken(w http.ResponseWriter, r *http.Request, purpose string) {
	var in struct {
		Email string `json:"email"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if len(email) > 254 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Invalid email")
		return
	}
	if m.Config.SMTPAddress == "" {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Email delivery is not configured")
		return
	}
	u, err := store.New(m.DB).UserByEmail(r.Context(), email)
	if err == pgx.ErrNoRows || (err == nil && purpose == "verify_email" && u.EmailVerified) {
		w.WriteHeader(202)
		return
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	if err = q.DeletePurposeTokens(r.Context(), store.DeletePurposeTokensParams{UserID: u.ID, Purpose: purpose}); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	token := httpx.Token()
	if err = q.CreateAccountToken(r.Context(), store.CreateAccountTokenParams{TokenHash: httpx.Hash(token), UserID: u.ID, Purpose: purpose, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	subject := "Verify your CardPlay account"
	if purpose == "reset_password" {
		subject = "Reset your CardPlay password"
	}
	body := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOpen %s and enter this token:\r\n%s\r\nIt expires in one hour.\r\n", email, m.Config.MailFrom, subject, m.Config.Origin, token)
	if err = m.sendMail(r.Context(), email, body); err != nil {
		httpx.Error(w, r, 503, "MAIL_UNAVAILABLE", "Email could not be delivered; try again")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	w.WriteHeader(202)
}

func (m *Module) ResendVerification(w http.ResponseWriter, r *http.Request) {
	m.issueAccountToken(w, r, "verify_email")
}
func (m *Module) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	m.issueAccountToken(w, r, "reset_password")
}

func (m *Module) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if len(in.Token) != 64 || len(in.Password) < 12 || len(in.Password) > 128 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Use a valid token and a 12-128 byte password")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	id, err := q.ConsumeAccountToken(r.Context(), store.ConsumeAccountTokenParams{TokenHash: httpx.Hash(in.Token), Purpose: "reset_password"})
	if err != nil {
		httpx.Error(w, r, 400, "TOKEN_INVALID", "Token is invalid or expired")
		return
	}
	if err = q.ChangePassword(r.Context(), store.ChangePasswordParams{ID: id, PasswordHash: PasswordHash(in.Password)}); err == nil {
		err = q.RevokeAllSessions(r.Context(), id)
	}
	if err == nil {
		// The reset token arrived by email, which proves the address.
		err = q.VerifyUser(r.Context(), id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]bool{"password_reset": true})
}

func (m *Module) Sessions(w http.ResponseWriter, r *http.Request) {
	items, err := store.New(m.DB).ListSessions(r.Context(), httpx.Actor(r).ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (m *Module) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "sessionID")
	if !httpx.UUID(id) {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Invalid session ID")
		return
	}
	n, err := store.New(m.DB).RevokeSession(r.Context(), store.RevokeSessionParams{ID: id, UserID: httpx.Actor(r).ID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, 404, "NOT_FOUND", "Session not found")
		return
	}
	w.WriteHeader(204)
}
func (m *Module) UpdateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisplayName string `json:"display_name"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var ok bool
	if in.DisplayName, ok = httpx.CleanText(in.DisplayName, 60); !ok {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Display name must be 1-60 characters without control characters")
		return
	}
	if err := store.New(m.DB).ChangeDisplayName(r.Context(), store.ChangeDisplayNameParams{ID: httpx.Actor(r).ID, DisplayName: in.DisplayName}); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"display_name": in.DisplayName})
}
func (m *Module) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if len(in.NewPassword) < 12 || len(in.NewPassword) > 128 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "New password must be 12-128 bytes")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	u, err := q.UserByIDForUpdate(r.Context(), httpx.Actor(r).ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !PasswordMatches(u.PasswordHash, in.CurrentPassword) {
		httpx.Error(w, r, 403, "INVALID_CREDENTIALS", "Incorrect current password")
		return
	}
	if err = q.ChangePassword(r.Context(), store.ChangePasswordParams{ID: u.ID, PasswordHash: PasswordHash(in.NewPassword)}); err == nil {
		err = q.RevokeAllSessions(r.Context(), u.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.Config.SecureCookie, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}
func (m *Module) DeleteMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	actor := httpx.Actor(r)
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	u, err := q.UserByIDForUpdate(r.Context(), actor.ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !PasswordMatches(u.PasswordHash, in.Password) {
		httpx.Error(w, r, 403, "INVALID_CREDENTIALS", "Incorrect password")
		return
	}
	// Deleting an account during a match abandons it (PRD P09).
	if err = matches.AbandonForDeparture(r.Context(), q, actor.ID); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if err = transferHostedRooms(r.Context(), q, actor.ID); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	// Only hosted waiting rooms with nobody else seated remain to close.
	closed, err := q.CloseHostedRooms(r.Context(), actor.ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	for _, roomID := range closed {
		if err = q.RevokeRoomInvitations(r.Context(), roomID); err == nil {
			err = q.Enqueue(r.Context(), store.EnqueueParams{RoomID: roomID, Kind: "room.updated", Payload: []byte(`{}`)})
		}
		if err != nil {
			httpx.DBError(w, r, err)
			return
		}
	}
	left, err := q.RemoveFromWaitingRooms(r.Context(), actor.ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	for _, roomID := range left {
		if err = q.ResetReady(r.Context(), roomID); err == nil {
			err = q.BumpRoom(r.Context(), roomID)
		}
		if err == nil {
			err = q.Enqueue(r.Context(), store.EnqueueParams{RoomID: roomID, Kind: "room.updated", Payload: []byte(`{}`)})
		}
		if err != nil {
			httpx.DBError(w, r, err)
			return
		}
	}
	steps := []func(context.Context, string) error{q.RevokeUserInvitations, q.DeleteFriendships, q.DeleteBlocks, q.DeleteMutes, q.DeleteAccountTokens, q.RevokeAllSessions}
	for _, step := range steps {
		if err = step(r.Context(), actor.ID); err != nil {
			httpx.DBError(w, r, err)
			return
		}
	}
	err = q.AnonymizeUser(r.Context(), store.AnonymizeUserParams{ID: actor.ID, PasswordHash: PasswordHash(httpx.Token())})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.Config.SecureCookie, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}

// transferHostedRooms passes each waiting room the user hosts to its
// longest-present other member, under the room lock like an explicit leave.
func transferHostedRooms(ctx context.Context, q *store.Queries, userID string) error {
	mine, err := q.MyRooms(ctx, userID)
	if err != nil {
		return err
	}
	for _, room := range mine {
		if room.HostID != userID || room.Status != "waiting" {
			continue
		}
		locked, err := q.LockRoom(ctx, room.ID)
		if err != nil {
			return err
		}
		if locked.HostID != userID || locked.Status != "waiting" {
			continue
		}
		members, err := q.Members(ctx, room.ID)
		if err != nil {
			return err
		}
		if next := rooms.NextHost(members, userID); next != nil {
			if err = q.SetRoomHost(ctx, store.SetRoomHostParams{ID: room.ID, HostID: next.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}
