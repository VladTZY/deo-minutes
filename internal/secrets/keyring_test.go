package secrets

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSecretsRoundTrip(t *testing.T) {
	keyring.MockInit()
	if IsSet(Gateway) {
		t.Fatal("should start empty")
	}
	if _, err := Get(Gateway); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := Set(Gateway, "k-123"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(Gateway); v != "k-123" {
		t.Fatalf("got %q", v)
	}
	if err := Delete(Gateway); err != nil {
		t.Fatal(err)
	}
	if err := Delete(Gateway); err != nil {
		t.Fatal("delete of missing secret must be a no-op")
	}
	if err := Set(Gateway, ""); err == nil {
		t.Fatal("empty value must be rejected")
	}
}

func TestAWS(t *testing.T) {
	keyring.MockInit()
	if err := SetAWS(AWSCredentials{AccessKeyID: "A"}); err == nil {
		t.Fatal("incomplete creds must be rejected")
	}
	want := AWSCredentials{AccessKeyID: "A", SecretAccessKey: "S"}
	if err := SetAWS(want); err != nil {
		t.Fatal(err)
	}
	got, err := GetAWS()
	if err != nil || got != want {
		t.Fatalf("got %+v %v", got, err)
	}
}
