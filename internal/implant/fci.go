package implant

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	MaxFCIArtifactSize = 32 * 1024 * 1024
	MaxFCIManifestSize = 64 * 1024
)

var ErrInvalidFCIArtifact = errors.New("invalid .fci artifact")

type FCIArtifactManifest struct {
	YAML    string `json:"yaml"`
	Slug    string `json:"slug"`
	SHA256  string `json:"sha256"`
	Size    int    `json:"size"`
	Runtime string `json:"runtime"`
}

type FCIArtifact struct {
	ManifestJSON json.RawMessage
	Manifest     FCIArtifactManifest
	Payload      []byte
	PayloadSHA   string
}

func ParseFCIArtifact(data []byte) (*FCIArtifact, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: artifact too short (%d bytes, need at least 4 for length prefix)",
			ErrInvalidFCIArtifact, len(data))
	}
	if len(data) > MaxFCIArtifactSize {
		return nil, fmt.Errorf("%w: artifact too large (%d bytes, max %d)",
			ErrInvalidFCIArtifact, len(data), MaxFCIArtifactSize)
	}

	headerLen := binary.BigEndian.Uint32(data[:4])
	if headerLen == 0 {
		return nil, fmt.Errorf("%w: manifest header length is zero", ErrInvalidFCIArtifact)
	}
	if headerLen > MaxFCIManifestSize {
		return nil, fmt.Errorf("%w: manifest header too large (%d bytes, max %d)",
			ErrInvalidFCIArtifact, headerLen, MaxFCIManifestSize)
	}
	if int(headerLen)+4 > len(data) {
		return nil, fmt.Errorf("%w: manifest header truncated (need %d bytes, have %d)",
			ErrInvalidFCIArtifact, headerLen+4, len(data))
	}

	manifestJSON := data[4 : 4+headerLen]
	payload := data[4+headerLen:]

	var m FCIArtifactManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest JSON parse failed: %v",
			ErrInvalidFCIArtifact, err)
	}
	if m.Slug == "" {
		return nil, fmt.Errorf("%w: manifest missing slug", ErrInvalidFCIArtifact)
	}
	if m.Runtime == "" {
		return nil, fmt.Errorf("%w: manifest missing runtime", ErrInvalidFCIArtifact)
	}
	if m.Size != len(payload) {
		return nil, fmt.Errorf("%w: payload size mismatch (header says %d, got %d)",
			ErrInvalidFCIArtifact, m.Size, len(payload))
	}

	gotSHA := sha256.Sum256(payload)
	gotHex := hex.EncodeToString(gotSHA[:])
	if m.SHA256 != "" && m.SHA256 != gotHex {
		return nil, fmt.Errorf("%w: payload SHA-256 mismatch (header says %s, got %s)",
			ErrInvalidFCIArtifact, m.SHA256, gotHex)
	}
	if m.SHA256 == "" {
		m.SHA256 = gotHex
	}

	return &FCIArtifact{
		ManifestJSON: manifestJSON,
		Manifest:     m,
		Payload:      payload,
		PayloadSHA:   gotHex,
	}, nil
}

func EncodeFCIArtifact(m FCIArtifactManifest, payload []byte, dst []byte) ([]byte, error) {
	if m.Runtime == "" {
		return nil, fmt.Errorf("%w: runtime is required", ErrInvalidFCIArtifact)
	}
	if m.Slug == "" {
		return nil, fmt.Errorf("%w: slug is required", ErrInvalidFCIArtifact)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: payload is empty", ErrInvalidFCIArtifact)
	}
	if len(payload) > MaxFCIArtifactSize {
		return nil, fmt.Errorf("%w: payload too large (%d bytes)", ErrInvalidFCIArtifact, len(payload))
	}

	sha := sha256.Sum256(payload)
	m.SHA256 = hex.EncodeToString(sha[:])
	m.Size = len(payload)

	manifestJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal manifest: %v", ErrInvalidFCIArtifact, err)
	}
	if len(manifestJSON) > MaxFCIManifestSize {
		return nil, fmt.Errorf("%w: manifest header exceeds %d bytes after marshal",
			ErrInvalidFCIArtifact, MaxFCIManifestSize)
	}

	total := 4 + len(manifestJSON) + len(payload)
	if cap(dst) < total {
		dst = make([]byte, 0, total)
	}
	dst = dst[:0]

	lenBuf := []byte{0, 0, 0, 0}
	binary.BigEndian.PutUint32(lenBuf, uint32(len(manifestJSON)))
	dst = append(dst, lenBuf...)
	dst = append(dst, manifestJSON...)
	dst = append(dst, payload...)

	return dst, nil
}

func SignFCIArtifact(manifestJSON []byte, signingKey string) (string, error) {
	if signingKey == "" {
		return "", errors.New("signing key is empty")
	}

	canonical := CanonicalizeFCIJSON(manifestJSON)
	mac := hmacSHA256([]byte(signingKey), canonical)
	return hex.EncodeToString(mac), nil
}

func VerifyFCIArtifactSignature(art *FCIArtifact, signature, signingKey string) error {
	if signingKey == "" {
		return errors.New("signing key is empty")
	}
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return errors.New("signature is not valid hex")
	}
	if len(sigBytes) != sha256.Size {
		return errors.New("signature length does not match HMAC-SHA256")
	}

	mac := hmacSHA256([]byte(signingKey), CanonicalizeFCIJSON(art.ManifestJSON))
	if !bytes.Equal(sigBytes, mac) {
		return errors.New("HMAC mismatch — manifest may have been tampered with")
	}
	return nil
}

func CanonicalizeFCIJSON(raw json.RawMessage) []byte {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
