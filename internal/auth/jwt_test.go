package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWT(t *testing.T) {
	userID := uuid.New()
	secret := "um-segredo-super-seguro"

	// Testar criação e validação bem-sucedida
	token, err := MakeJWT(userID, secret, time.Hour)
	if err != nil {
		t.Fatalf("erro inesperado ao criar JWT: %v", err)
	}

	validatedID, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("erro inesperado ao validar JWT: %v", err)
	}

	if validatedID != userID {
		t.Fatalf("esperava o userID %v, mas obteve %v", userID, validatedID)
	}

	// Testar com um secret errado
	_, err = ValidateJWT(token, "segredo-errado")
	if err == nil {
		t.Fatalf("esperava um erro ao validar JWT com o secret errado, mas obteve nil")
	}

	// Testar com um token expirado
	expiredToken, err := MakeJWT(userID, secret, -time.Hour) // Tempo negativo para expirar imediatamente
	if err != nil {
		t.Fatalf("erro inesperado ao criar JWT expirado: %v", err)
	}

	_, err = ValidateJWT(expiredToken, secret)
	if err == nil {
		t.Fatalf("esperava um erro ao validar JWT expirado, mas obteve nil")
	}
}



