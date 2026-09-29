package httpx

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Identity struct {
	ID, Handle, DisplayName, Email, SessionHash string
	Verified                                    bool
}
type identityKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}
func Actor(r *http.Request) Identity { v, _ := r.Context().Value(identityKey{}).(Identity); return v }
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Actor(r).ID == "" {
			Error(w, r, 401, "UNAUTHENTICATED", "Sign in to continue")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func Verified(next http.Handler) http.Handler {
	return Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Actor(r).Verified {
			Error(w, r, 403, "EMAIL_UNVERIFIED", "Verify your email before using rooms and friends")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	JSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": middleware.GetReqID(r.Context())}})
}
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		Error(w, r, 400, "INVALID_REQUEST", "Expected a valid JSON object")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		Error(w, r, 400, "INVALID_REQUEST", "Expected one JSON object")
		return false
	}
	return true
}
func DBError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		Error(w, r, 404, "NOT_FOUND", "Resource not found")
		return
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505":
			Error(w, r, 409, "CONFLICT", "That value already exists")
			return
		case "23503", "23514", "22P02":
			Error(w, r, 400, "INVALID_REQUEST", "Invalid resource or value")
			return
		}
	}
	// Log SQLSTATE only: driver errors may contain email addresses or credentials.
	code := "unknown"
	if pe != nil {
		code = pe.Code
	}
	slog.Error("database operation failed", "request_id", middleware.GetReqID(r.Context()), "sqlstate", code)
	Error(w, r, 500, "INTERNAL", "The request could not be completed")
}
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func UUID(s string) bool { return uuidPattern.MatchString(s) }
