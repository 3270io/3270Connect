package oidc

import (
	"regexp"
	"testing"
)

// The verifier/challenge pair is the worked example in RFC 7636 appendix B.
// Checking against it, rather than re-deriving the hash in the test, is what
// catches a challenge that is self-consistent but not what an identity
// provider will compute.
func TestCodeChallengeMatchesRFC7636Example(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if got := CodeChallenge(verifier); got != want {
		t.Fatalf("CodeChallenge(%q) = %q, want %q", verifier, got, want)
	}
}

func TestRandomStringIsUnpaddedURLSafeAndUnique(t *testing.T) {
	urlSafe := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

	// 32 bytes encode to 43 unpadded base64url characters, the length RFC 7636
	// requires of a verifier at minimum.
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, err := RandomString(32)
		if err != nil {
			t.Fatalf("RandomString: %v", err)
		}
		if len(s) != 43 {
			t.Fatalf("len(RandomString(32)) = %d, want 43", len(s))
		}
		if !urlSafe.MatchString(s) {
			t.Fatalf("RandomString produced %q, which is not unpadded base64url", s)
		}
		if seen[s] {
			t.Fatalf("RandomString returned %q twice; state, nonce and verifier must not repeat", s)
		}
		seen[s] = true
	}
}
