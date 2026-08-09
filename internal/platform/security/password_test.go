package security

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("empty hash")
	}
	ok, err := VerifyPassword(hash, "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected valid password to verify")
	}
	ok, _ = VerifyPassword(hash, "wrong")
	if ok {
		t.Fatal("wrong password must not verify")
	}
}

func TestTokenHashDeterministic(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Fatal("token hash must be deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatal("different tokens must hash differently")
	}
}
