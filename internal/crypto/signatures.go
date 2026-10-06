package crypto

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Desvelao/dsipy/internal/canonical"
)

// SignEndorsement signs an endorsement and returns the lowercase hex signature.
func SignEndorsement(priv ed25519.PrivateKey, endorseeKeyB64 string) string {
	return hex.EncodeToString(ed25519.Sign(priv, canonical.EndorsementString(endorseeKeyB64)))
}

// SignFeedItem signs a feed item and returns the lowercase hex signature.
func SignFeedItem(priv ed25519.PrivateKey, pubDate, title, descriptionPlain string) string {
	return hex.EncodeToString(ed25519.Sign(priv, canonical.FeedString(pubDate, title, descriptionPlain)))
}

// DecodeSignatureHex decodes a lowercase hex signature; uppercase, whitespace
// and odd length are rejected.
func DecodeSignatureHex(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("signature must be lowercase hexadecimal")
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, errors.New("signature must be lowercase hexadecimal")
		}
	}
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("non-hexadecimal number found in fromhex() arg at position %d", len(s))
	}
	return hex.DecodeString(s)
}

func verify(pub ed25519.PublicKey, msg []byte, signatureHex string) bool {
	sig, err := DecodeSignatureHex(signatureHex)
	if err != nil || len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// VerifyEndorsementSignature verifies an endorsement signature.
func VerifyEndorsementSignature(pub ed25519.PublicKey, endorseeKeyB64, signatureHex string) bool {
	return verify(pub, canonical.EndorsementString(endorseeKeyB64), signatureHex)
}

// VerifyFeedSignature verifies a feed item signature.
func VerifyFeedSignature(pub ed25519.PublicKey, pubDate, title, descriptionPlain, signatureHex string) bool {
	return verify(pub, canonical.FeedString(pubDate, title, descriptionPlain), signatureHex)
}
