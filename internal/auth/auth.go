package auth

import (
	"fmt"
	"time"
	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// HashPassword cria um hash Argon2id a partir da password
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// CheckPasswordHash compara a password em texto limpo com o hash do banco
func CheckPasswordHash(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}

// MakeJWT cria e devolve um JWT assinado
func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	// A chave secreta deve ser convertida para uma slice de bytes
	signingKey := []byte(tokenSecret)

	// Definir as claims (reivindicações) do token
	claims := jwt.RegisteredClaims{
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(expiresIn)),
		Subject:   userID.String(),
	}

	// Criar o token com o método de assinatura HS256 e as claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	
	// Assinar o token e devolvê-lo
	return token.SignedString(signingKey)
}

// ValidateJWT valida a assinatura do JWT e extrai o UUID do utilizador
func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claimsStruct := &jwt.RegisteredClaims{}

	// Fazer o parse e validar o token
	token, err := jwt.ParseWithClaims(
		tokenString,
		claimsStruct,
		func(token *jwt.Token) (interface{}, error) {
			// A função de callback deve devolver a mesma chave (em []byte) usada na assinatura
			return []byte(tokenSecret), nil
		},
	)

	if err != nil {
		return uuid.Nil, err
	}

	// Obter o Subject (que contém o userID em formato string)
	userIDString, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, err
	}

	// Converter a string de volta para uuid.UUID
	userID, err := uuid.Parse(userIDString)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ID de utilizador inválido no token: %w", err)
	}

	return userID, nil
}






