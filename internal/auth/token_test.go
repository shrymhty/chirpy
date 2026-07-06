package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMakeJWT(t *testing.T) {
	userId := uuid.New()
	secret := "my-secret"
	duration := time.Hour

	tokenString, err := MakeJWT(userId, secret, duration)
	if err != nil {
		t.Fatalf("MakeJWT failed %v", err)
	}

	if tokenString == "" {
		t.Fatalf("Expected a token string, got empty string")
	}

	parsedID, err := ValidateJWT(tokenString, secret)
	if err != nil {
		t.Fatalf("unable to validate generated token string: %v", err)
	}

	if parsedID != userId {
		t.Errorf("expected userID %v, got %v", userId, parsedID)
	}
}