package yeguo

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestDecryptMedia(t *testing.T) {
	pngSample := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d}

	// Plaintext image should pass through untouched.
	got, err := DecryptMedia(pngSample)
	if err != nil {
		t.Fatalf("DecryptMedia plaintext failed: %v", err)
	}
	if !bytes.Equal(got, pngSample) {
		t.Fatalf("plaintext mismatch: got %x want %x", got, pngSample)
	}

	// AES-128-CBC + PKCS7 encrypted PNG should decrypt to pngSample.
	pad := aes.BlockSize - (len(pngSample) % aes.BlockSize)
	padded := append(append([]byte{}, pngSample...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher([]byte(mediaKey))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(mediaIV)).CryptBlocks(ct, padded)

	gotDec, err := DecryptMedia(ct)
	if err != nil {
		t.Fatalf("DecryptMedia ciphertext failed: %v", err)
	}
	if !bytes.Equal(gotDec, pngSample) {
		t.Fatalf("decrypted mismatch: got %x want %x", gotDec, pngSample)
	}
}
