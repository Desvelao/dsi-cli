// Package crypto implements the Ed25519 operations of DSI: key generation and
// encoding, loading, signing and verification.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

const deserializeMsg = "Could not deserialize key data. The data may be in an incorrect format, " +
	"it may be encrypted with an unsupported algorithm, or it may be an unsupported key type " +
	"(e.g. EC curves with explicit parameters)."

const pemMsg = "Unable to load PEM file. See https://cryptography.io/en/latest/faq/#why-can-t-i-import-my-pem-file for more details."

// GenerateKeypair generates an Ed25519 keypair and returns the PKCS8 private
// PEM, the SPKI public PEM and the Base64 of the public key DER.
func GenerateKeypair() (privPEM, pubPEM []byte, pubB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, "", err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, "", err
	}
	pubDER, err := PublicKeyToDER(pub)
	if err != nil {
		return nil, nil, "", err
	}
	privPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return privPEM, pubPEM, base64.StdEncoding.EncodeToString(pubDER), nil
}

// WritePrivateKey writes a private key PEM, created with mode 0600 from the
// start. Without force it fails with os.ErrExist if the file exists.
func WritePrivateKey(path string, pemBytes []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	// also tightens a pre-existing file when forced
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	_, err = f.Write(pemBytes)
	return err
}

// ActionGenerateKeypair generates a keypair and saves it to the given PEM
// files. Without force it fails if either file exists.
func ActionGenerateKeypair(priv, pub string, force bool) (privPEM, pubPEM []byte, pubB64 string, err error) {
	if !force {
		for _, p := range []string{priv, pub} {
			if _, statErr := os.Stat(p); statErr == nil {
				return nil, nil, "", &ExistsError{Path: p}
			}
		}
	}
	privPEM, pubPEM, pubB64, err = GenerateKeypair()
	if err != nil {
		return nil, nil, "", err
	}
	if err := WritePrivateKey(priv, privPEM, force); err != nil {
		return nil, nil, "", err
	}
	if err := os.WriteFile(pub, pubPEM, 0o644); err != nil {
		return nil, nil, "", err
	}
	return privPEM, pubPEM, pubB64, nil
}

// PublicKeyToDER exports a public key as SubjectPublicKeyInfo DER.
func PublicKeyToDER(pub ed25519.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}

// PublicKeyToB64DER exports a public key as Base64 of its SPKI DER.
func PublicKeyToB64DER(pub ed25519.PublicKey) (string, error) {
	der, err := PublicKeyToDER(pub)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// B64DERToPublicKeyPEM converts a Base64 DER public key to PEM text.
func B64DERToPublicKeyPEM(content string) (string, error) {
	pub, err := LoadPublicKeyB64DER(content)
	if err != nil {
		return "", err
	}
	der, err := PublicKeyToDER(pub)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// LoadPrivateKeyPEM loads an unencrypted PKCS8 Ed25519 private key.
func LoadPrivateKeyPEM(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New(pemMsg + " MalformedFraming")
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, errors.New("Password was not given but private key is encrypted")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s Details: %v", deserializeMsg, err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("Private key is not an Ed25519 key")
	}
	return priv, nil
}

// LoadPublicKeyPEM loads an SPKI Ed25519 public key.
func LoadPublicKeyPEM(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New(pemMsg + " MalformedFraming")
	}
	return parsePublicDER(block.Bytes)
}

func parsePublicDER(der []byte) (ed25519.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("%s Details: %v", deserializeMsg, err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("Public key is not an Ed25519 key")
	}
	return pub, nil
}

// DecodeB64Strict decodes standard Base64 like Python's
// base64.b64decode(validate=True), including its error messages.
func DecodeB64Strict(s string) ([]byte, error) {
	if err := checkASCII(s); err != nil {
		return nil, fmt.Errorf("Invalid Base64 value: %s", err)
	}
	if err := strictBase64Check(s); err != nil {
		return nil, fmt.Errorf("Invalid Base64 value: %s", err)
	}
	return base64.StdEncoding.DecodeString(s)
}

// checkASCII reproduces the UnicodeEncodeError text of str.encode("ascii").
func checkASCII(s string) error {
	runes := []rune(s)
	start := -1
	for i, r := range runes {
		if r > 127 {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := start + 1
	for end < len(runes) && runes[end] > 127 {
		end++
	}
	if end-start == 1 {
		return fmt.Errorf("'ascii' codec can't encode character '%s' in position %d: ordinal not in range(128)",
			escapeRune(runes[start]), start)
	}
	return fmt.Errorf("'ascii' codec can't encode characters in position %d-%d: ordinal not in range(128)", start, end-1)
}

func escapeRune(r rune) string {
	switch {
	case r <= 0xff:
		return fmt.Sprintf(`\x%02x`, r)
	case r <= 0xffff:
		return fmt.Sprintf(`\u%04x`, r)
	default:
		return fmt.Sprintf(`\U%08x`, r)
	}
}

// strictBase64Check mirrors CPython's binascii.a2b_base64(strict_mode=True)
// validation (the error messages included).
func strictBase64Check(s string) error {
	if len(s) > 0 && s[0] == '=' {
		return errors.New("Leading padding not allowed")
	}
	quad, pads, dataChars := 0, 0, 0
	paddingStarted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '=' {
			paddingStarted = true
			if quad >= 2 {
				pads++
				if quad+pads >= 4 {
					if i+1 < len(s) {
						return errors.New("Excess data after padding")
					}
					return nil
				}
			} else if quad == 0 {
				return errors.New("Excess padding not allowed")
			}
			continue
		}
		if !isB64Char(c) {
			return errors.New("Only base64 data is allowed")
		}
		if paddingStarted {
			return errors.New("Discontinuous padding not allowed")
		}
		pads = 0
		dataChars++
		quad = (quad + 1) % 4
	}
	switch quad {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("Invalid base64-encoded string: number of data characters (%d) cannot be 1 more than a multiple of 4", dataChars)
	default:
		return errors.New("Incorrect padding")
	}
}

func isB64Char(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/'
}

// LoadPublicKeyB64DER loads an Ed25519 public key from Base64-encoded DER.
func LoadPublicKeyB64DER(b64 string) (ed25519.PublicKey, error) {
	der, err := DecodeB64Strict(b64)
	if err != nil {
		return nil, err
	}
	return parsePublicDER(der)
}

// ExistsError means a key file already exists (Python's FileExistsError).
type ExistsError struct{ Path string }

func (e *ExistsError) Error() string { return fmt.Sprintf("'%s' already exists", e.Path) }

// Is makes errors.Is(err, os.ErrExist) true.
func (e *ExistsError) Is(target error) bool { return target == os.ErrExist }

// IsExist reports whether err means a key file already exists.
func IsExist(err error) bool { return errors.Is(err, os.ErrExist) }
