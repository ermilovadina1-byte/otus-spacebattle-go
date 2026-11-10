package auth

import (
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	issuerID  = "spacebattle-auth"
	audience  = "spacebattle-game"
	tokenType = "game-access+jwt"
)

var ErrInvalidToken = errors.New("invalid game access token")

// GameClaims grant one authenticated user access to one game.
type GameClaims struct {
	GameID string `json:"game_id"`
	jwt.RegisteredClaims
}

type TokenIssuer struct {
	privateKey ed25519.PrivateKey
	ttl        time.Duration
}

func NewTokenIssuer(privateKey ed25519.PrivateKey, ttl time.Duration) (*TokenIssuer, error) {
	if len(privateKey) != ed25519.PrivateKeySize || ttl < time.Second || ttl > time.Hour {
		return nil, errors.New("invalid token issuer configuration")
	}
	return &TokenIssuer{privateKey: append(ed25519.PrivateKey(nil), privateKey...), ttl: ttl}, nil
}

func (issuer *TokenIssuer) Issue(userID, gameID string) (string, error) {
	if issuer == nil || userID == "" || gameID == "" {
		return "", errors.New("user and game are required")
	}
	now := time.Now().UTC()
	claims := GameClaims{
		GameID: gameID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuerID,
			Audience:  jwt.ClaimStrings{audience},
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(issuer.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["typ"] = tokenType
	return token.SignedString(issuer.privateKey)
}

func (issuer *TokenIssuer) TTL() time.Duration { return issuer.ttl }

// TokenVerifier needs only the public key; it cannot mint game tokens.
type TokenVerifier struct{ publicKey ed25519.PublicKey }

func NewTokenVerifier(publicKey ed25519.PublicKey) (*TokenVerifier, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("invalid token verification key")
	}
	return &TokenVerifier{publicKey: append(ed25519.PublicKey(nil), publicKey...)}, nil
}

func (verifier *TokenVerifier) Verify(raw, gameID string) (string, error) {
	if verifier == nil || raw == "" || gameID == "" {
		return "", ErrInvalidToken
	}
	claims := new(GameClaims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return verifier.publicKey, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(issuerID),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil || !token.Valid || token.Header["typ"] != tokenType ||
		claims.Subject == "" || claims.GameID != gameID || claims.IssuedAt == nil {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}
