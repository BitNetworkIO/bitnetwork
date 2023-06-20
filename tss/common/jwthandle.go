package common

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	jwtExpiryTimeout = 60 * time.Second
)

type JwtHandler struct {
	keyFunc func(token *jwt.Token) (interface{}, error)
	handler http.Handler
}

func NewJwtHandler(handle http.Handler, jetSecretKey string) (http.Handler, error) {
	jwtHandler := &JwtHandler{
		keyFunc: func(token *jwt.Token) (interface{}, error) {
			return []byte(jetSecretKey), nil
		},
		handler: handle,
	}
	return jwtHandler, nil
}

// ServeHTTP implements http.Handler
func (handler *JwtHandler) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	var (
		strToken string
		claims   jwt.RegisteredClaims
	)
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		strToken = strings.TrimPrefix(auth, "Bearer ")
	}
	if len(strToken) == 0 {
		http.Error(out, "missing token", http.StatusUnauthorized)
		return
	}
	// We explicitly set only HS256 allowed, and also disables the
	// claim-check: the RegisteredClaims internally requires 'iat' to
	// be no later than 'now', but we allow for a bit of drift.
	token, err := jwt.ParseWithClaims(strToken, &claims, handler.keyFunc,
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithoutClaimsValidation())

	switch {
	case err != nil:
		http.Error(out, err.Error(), http.StatusUnauthorized)
	case !token.Valid:
		http.Error(out, "invalid token", http.StatusUnauthorized)
	case !claims.VerifyExpiresAt(time.Now(), false): // optional
		http.Error(out, "token is expired", http.StatusUnauthorized)
	case claims.IssuedAt == nil:
		http.Error(out, "missing issued-at", http.StatusUnauthorized)
	case time.Since(claims.IssuedAt.Time) > jwtExpiryTimeout:
		http.Error(out, "stale token", http.StatusUnauthorized)
	case time.Until(claims.IssuedAt.Time) > jwtExpiryTimeout:
		http.Error(out, "future token", http.StatusUnauthorized)
	default:
		handler.handler.ServeHTTP(out, r)
	}
}
