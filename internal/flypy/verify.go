package flypy

import "crypto/ed25519"

// VerifyArtifactSignature verifies the Ed25519 signature on a local
// Artifact using the public key provided. The signature was produced
// at build time over the artifact's determinism hash.
func VerifyArtifactSignature(a *Artifact, publicKey []byte) error {
	if a == nil {
		return errNilArtifact
	}
	if len(a.Signature) == 0 {
		return errNoSignature
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return errInvalidPublicKey
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(a.DeterminismHash), a.Signature) {
		return errBadSignature
	}
	return nil
}

// SignArtifact produces an Ed25519 signature over the determinism hash.
// Used by the build path; exposed here so callers don't have to know
// the internal signing scheme.
func SignArtifact(privateKey []byte, determinismHash string) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errInvalidPrivateKey
	}
	return ed25519.Sign(ed25519.PrivateKey(privateKey), []byte(determinismHash)), nil
}

var (
	errNilArtifact       = &verifyError{msg: "nil artifact"}
	errNoSignature       = &verifyError{msg: "artifact has no signature"}
	errInvalidPublicKey  = &verifyError{msg: "invalid public key size"}
	errInvalidPrivateKey = &verifyError{msg: "invalid private key size"}
	errBadSignature      = &verifyError{msg: "signature verification failed"}
)

type verifyError struct{ msg string }

func (e *verifyError) Error() string { return e.msg }
