package utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"strings"

	"github.com/veops/oneterm/pkg/config"
)

// gcmPrefix marks ciphertexts produced with AES-GCM (authenticated, random
// per-message nonce). Legacy ciphertexts produced by AES-CBC with the static
// configured IV have no prefix and are pure base64 — they continue to decrypt
// for backward compatibility, but every new write uses GCM.
const gcmPrefix = "v1:"

var (
	key, iv []byte
)

func init() {
	key = []byte(config.Cfg.Auth.Aes.Key)
	iv = []byte(config.Cfg.Auth.Aes.Iv)
}

// EncryptAES encrypts plainText with AES-GCM using a fresh random nonce per
// call and returns "v1:<base64(nonce||ciphertext||tag)>".
//
// If the configured key length is invalid (not 16/24/32 bytes) the function
// falls back to the legacy CBC+static-IV path so the application still boots
// in misconfigured environments. Operators should be alerted by failing tests.
func EncryptAES(plainText string) string {
	block, err := aes.NewCipher(key)
	if err != nil {
		return legacyEncryptCBC(plainText)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return legacyEncryptCBC(plainText)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return legacyEncryptCBC(plainText)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plainText), nil)
	return gcmPrefix + base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

// DecryptAES decrypts ciphertexts produced by either EncryptAES (with the
// "v1:" prefix) or the historical CBC+static-IV format (raw base64).
func DecryptAES(cipherText string) string {
	if strings.HasPrefix(cipherText, gcmPrefix) {
		return decryptGCM(cipherText[len(gcmPrefix):])
	}
	return legacyDecryptCBC(cipherText)
}

func decryptGCM(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	if len(raw) < gcm.NonceSize() {
		return ""
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func legacyEncryptCBC(plainText string) string {
	block, _ := aes.NewCipher(key)
	bs := []byte(plainText)
	bs = paddingPKCS7(bs, aes.BlockSize)

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(bs, bs)

	return base64.StdEncoding.EncodeToString(bs)
}

func legacyDecryptCBC(cipherText string) string {
	bs, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil || len(bs) == 0 || len(bs)%aes.BlockSize != 0 {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(bs, bs)

	return string(unPaddingPKCS7(bs))
}

func paddingPKCS7(plaintext []byte, blockSize int) []byte {
	paddingSize := blockSize - len(plaintext)%blockSize
	paddingText := bytes.Repeat([]byte{byte(paddingSize)}, paddingSize)
	return append(plaintext, paddingText...)
}

func unPaddingPKCS7(s []byte) []byte {
	length := len(s)
	if length == 0 {
		return s
	}
	unPadding := int(s[length-1])
	if unPadding > length {
		return s
	}
	return s[:(length - unPadding)]
}
