package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type JWTAuthenticator struct {
	jwksURL string
	issuer  string
	keys    map[string]*rsa.PublicKey
	mu      sync.RWMutex
}

func NewJWTAuthenticator(jwksURL, issuer string) *JWTAuthenticator {
	a := &JWTAuthenticator{
		jwksURL: jwksURL,
		issuer:  issuer,
		keys:    make(map[string]*rsa.PublicKey),
	}
	go a.fetchKeys()
	return a
}

func (a *JWTAuthenticator) fetchKeys() {
	for {
		resp, err := http.Get(a.jwksURL)
		if err != nil {
			time.Sleep(1 * time.Minute)
			continue
		}

		var jwks struct {
			Keys []struct {
				KID string `json:"kid"`
				Kty string `json:"kty"`
				N   string `json:"n"`
				E   string `json:"e"`
			} `json:"keys"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
			_ = resp.Body.Close()
			time.Sleep(1 * time.Minute)
			continue
		}
		_ = resp.Body.Close()

		newKeys := make(map[string]*rsa.PublicKey)
		for _, key := range jwks.Keys {
			if key.Kty != "RSA" {
				continue
			}
			nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
			if err != nil {
				continue
			}
			n := new(big.Int)
			n.SetBytes(nBytes)
			eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
			if err != nil {
				continue
			}
			e := new(big.Int)
			e.SetBytes(eBytes)
			if !e.IsInt64() {
				continue
			}
			publicKey := &rsa.PublicKey{
				N: n,
				E: int(e.Int64()),
			}
			newKeys[key.KID] = publicKey
		}

		a.mu.Lock()
		a.keys = newKeys
		a.mu.Unlock()

		time.Sleep(24 * time.Hour)
	}
}

func (a *JWTAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	tokenString := reqHeaders.Get("Authorization")
	if tokenString == "" {
		return nil, &auth.UnauthenticatedError{Msg: "missing authorization header"}
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid header not found")
		}
		a.mu.RLock()
		defer a.mu.RUnlock()
		key, found := a.keys[kid]
		if !found {
			return nil, fmt.Errorf("key not found for kid: %s", kid)
		}
		return key, nil
	})

	if err != nil {
		return nil, &auth.UnauthenticatedError{Msg: "invalid token: " + err.Error()}
	}

	if !token.Valid {
		return nil, &auth.UnauthenticatedError{Msg: "invalid token"}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, &auth.UnauthenticatedError{Msg: "invalid claims format"}
	}
	issuer, err := claims.GetIssuer()
	if err != nil || issuer != a.issuer {
		return nil, &auth.UnauthenticatedError{Msg: "invalid issuer"}
	}
	subject, err := claims.GetSubject()
	if err != nil {
		return nil, &auth.UnauthenticatedError{Msg: "missing subject"}
	}

	return &SimpleSession{
		P: auth.Principal{
			User: auth.User{
				ID: subject,
			},
		},
	}, nil
}

func (a *JWTAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	return nil
}
