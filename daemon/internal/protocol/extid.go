package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// ExtensionIDFromKey derives a Chrome extension ID from the base64 public key in the manifest's
// "key" field: the first 128 bits of SHA-256 over the DER key, written as hex digits shifted to a-p.
func ExtensionIDFromKey(b64 string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	hx := hex.EncodeToString(sum[:16])
	id := make([]byte, len(hx))
	for i := 0; i < len(hx); i++ {
		c := hx[i]
		if c >= 'a' {
			id[i] = 'a' + 10 + (c - 'a')
		} else {
			id[i] = 'a' + (c - '0')
		}
	}
	return string(id), nil
}
