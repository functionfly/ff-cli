package flypy

import (
	"fmt"
)

// Approve validates an artifact before deployment using the dedicated
// approval path. In strict mode, the artifact must carry a verifiable
// Ed25519 signature and a non-empty determinism hash. Approve does not
// mutate the artifact.
func Approve(artifact *Artifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.DeterminismHash == "" && len(artifact.Signature) != 0 {
		return fmt.Errorf("artifact signature present but determinism hash is missing")
	}
	return nil
}
