package pveweb

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("password hash contains the plaintext password")
	}
	if !validPasswordHash(hash) {
		t.Fatalf("generated hash is invalid: %q", hash)
	}
	if !verifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password was rejected")
	}
	if verifyPassword(hash, "wrong password") {
		t.Fatal("wrong password was accepted")
	}
}

func TestPasswordHashRejectsShortPassword(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("short password was accepted")
	}
}
