package implant

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestParseFCIArtifact_ValidArtifact(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "name: test\nversion: 1.0.0",
		Slug:    "test-implant",
		Runtime: "node22",
		Size:    10,
	}
	payload := []byte("0123456789")

	manifestJSON, _ := json.Marshal(manifest)
	artifact := make([]byte, 0, 4+len(manifestJSON)+len(payload))
	artifact = append(artifact, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(artifact[:4], uint32(len(manifestJSON)))
	artifact = append(artifact, manifestJSON...)
	artifact = append(artifact, payload...)

	art, err := ParseFCIArtifact(artifact)
	if err != nil {
		t.Fatalf("ParseFCIArtifact failed: %v", err)
	}

	if art.Manifest.Slug != "test-implant" {
		t.Errorf("Slug = %q, want test-implant", art.Manifest.Slug)
	}
	if art.Manifest.Runtime != "node22" {
		t.Errorf("Runtime = %q, want node22", art.Manifest.Runtime)
	}
	if art.Manifest.Size != 10 {
		t.Errorf("Size = %d, want 10", art.Manifest.Size)
	}
}

func TestParseFCIArtifact_TooShort(t *testing.T) {
	_, err := ParseFCIArtifact([]byte{0, 0})
	if err == nil {
		t.Error("ParseFCIArtifact should fail for data shorter than 4 bytes")
	}
}

func TestParseFCIArtifact_EmptyHeader(t *testing.T) {
	artifact := []byte{0, 0, 0, 0}
	_, err := ParseFCIArtifact(artifact)
	if err == nil {
		t.Error("ParseFCIArtifact should fail for zero header length")
	}
}

func TestParseFCIArtifact_TruncatedHeader(t *testing.T) {
	artifact := []byte{0, 0, 0, 10, 1, 2, 3}
	_, err := ParseFCIArtifact(artifact)
	if err == nil {
		t.Error("ParseFCIArtifact should fail for truncated header")
	}
}

func TestParseFCIArtifact_InvalidJSON(t *testing.T) {
	artifact := make([]byte, 4+10)
	binary.BigEndian.PutUint32(artifact[:4], uint32(10))
	copy(artifact[4:], []byte("not json{"))

	_, err := ParseFCIArtifact(artifact)
	if err == nil {
		t.Error("ParseFCIArtifact should fail for invalid JSON")
	}
}

func TestParseFCIArtifact_MissingSlug(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "test",
		Slug:    "",
		Runtime: "node22",
	}
	manifestJSON, _ := json.Marshal(manifest)

	artifact := make([]byte, 4+len(manifestJSON))
	binary.BigEndian.PutUint32(artifact[:4], uint32(len(manifestJSON)))
	copy(artifact[4:], manifestJSON)

	_, err := ParseFCIArtifact(artifact)
	if err == nil {
		t.Error("ParseFCIArtifact should fail for missing slug")
	}
}

func TestParseFCIArtifact_MissingRuntime(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "test",
		Slug:    "test-slug",
		Runtime: "",
	}
	manifestJSON, _ := json.Marshal(manifest)

	artifact := make([]byte, 4+len(manifestJSON))
	binary.BigEndian.PutUint32(artifact[:4], uint32(len(manifestJSON)))
	copy(artifact[4:], manifestJSON)

	_, err := ParseFCIArtifact(artifact)
	if err == nil {
		t.Error("ParseFCIArtifact should fail for missing runtime")
	}
}

func TestParseFCIArtifact_SHA256Mismatch(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "test",
		Slug:    "test-slug",
		Runtime: "node22",
		Size:    10,
		SHA256:  "wronghashwronghashwronghashwron",
	}
	manifestJSON, _ := json.Marshal(manifest)
	payload := []byte("0123456789")

	artifact := make([]byte, 4+len(manifestJSON)+len(payload))
	binary.BigEndian.PutUint32(artifact[:4], uint32(len(manifestJSON)))
	copy(artifact[4:4+len(manifestJSON)], manifestJSON)
	copy(artifact[4+len(manifestJSON):], payload)

	_, err := ParseFCIArtifact(artifact)
	if err == nil {
		t.Error("ParseFCIArtifact should fail for SHA256 mismatch")
	}
}

func TestEncodeFCIArtifact(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "name: test",
		Slug:    "test-slug",
		Runtime: "node22",
	}
	payload := []byte("testpayload")

	encoded, err := EncodeFCIArtifact(manifest, payload, nil)
	if err != nil {
		t.Fatalf("EncodeFCIArtifact failed: %v", err)
	}

	art, err := ParseFCIArtifact(encoded)
	if err != nil {
		t.Fatalf("Failed to parse encoded artifact: %v", err)
	}

	if art.Manifest.Slug != "test-slug" {
		t.Errorf("Slug = %q, want test-slug", art.Manifest.Slug)
	}
	if art.Manifest.Runtime != "node22" {
		t.Errorf("Runtime = %q, want node22", art.Manifest.Runtime)
	}
}

func TestEncodeFCIArtifact_EmptyPayload(t *testing.T) {
	manifest := FCIArtifactManifest{
		Slug:    "test-slug",
		Runtime: "node22",
	}
	_, err := EncodeFCIArtifact(manifest, []byte{}, nil)
	if err == nil {
		t.Error("EncodeFCIArtifact should fail for empty payload")
	}
}

func TestEncodeFCIArtifact_MissingSlug(t *testing.T) {
	manifest := FCIArtifactManifest{
		Slug:    "",
		Runtime: "node22",
	}
	_, err := EncodeFCIArtifact(manifest, []byte("test"), nil)
	if err == nil {
		t.Error("EncodeFCIArtifact should fail for missing slug")
	}
}

func TestEncodeFCIArtifact_MissingRuntime(t *testing.T) {
	manifest := FCIArtifactManifest{
		Slug:    "test-slug",
		Runtime: "",
	}
	_, err := EncodeFCIArtifact(manifest, []byte("test"), nil)
	if err == nil {
		t.Error("EncodeFCIArtifact should fail for missing runtime")
	}
}

func TestSignAndVerifyFCIArtifact(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "name: test",
		Slug:    "test-slug",
		Runtime: "node22",
	}
	payload := []byte("testpayload")

	encoded, _ := EncodeFCIArtifact(manifest, payload, nil)
	art, _ := ParseFCIArtifact(encoded)

	signingKey := "my-secret-key"
	signature, err := SignFCIArtifact(art.ManifestJSON, signingKey)
	if err != nil {
		t.Fatalf("SignFCIArtifact failed: %v", err)
	}

	err = VerifyFCIArtifactSignature(art, signature, signingKey)
	if err != nil {
		t.Errorf("VerifyFCIArtifactSignature failed: %v", err)
	}
}

func TestSignFCIArtifact_EmptyKey(t *testing.T) {
	_, err := SignFCIArtifact([]byte("{}"), "")
	if err == nil {
		t.Error("SignFCIArtifact should fail for empty key")
	}
}

func TestVerifyFCIArtifactSignature_WrongKey(t *testing.T) {
	manifest := FCIArtifactManifest{
		YAML:    "name: test",
		Slug:    "test-slug",
		Runtime: "node22",
	}
	payload := []byte("testpayload")

	encoded, _ := EncodeFCIArtifact(manifest, payload, nil)
	art, _ := ParseFCIArtifact(encoded)

	signature, _ := SignFCIArtifact(art.ManifestJSON, "key1")

	err := VerifyFCIArtifactSignature(art, signature, "key2")
	if err == nil {
		t.Error("VerifyFCIArtifactSignature should fail with wrong key")
	}
}

func TestVerifyFCIArtifactSignature_InvalidHex(t *testing.T) {
	art := &FCIArtifact{
		ManifestJSON: []byte("{}"),
	}
	err := VerifyFCIArtifactSignature(art, "not-hex!", "key")
	if err == nil {
		t.Error("VerifyFCIArtifactSignature should fail for invalid hex")
	}
}

func TestCanonicalizeFCIJSON(t *testing.T) {
	original := []byte(`{"b":2,"a":1}`)
	canonical := CanonicalizeFCIJSON(original)

	var origMap, canonMap map[string]interface{}
	json.Unmarshal(original, &origMap)
	json.Unmarshal(canonical, &canonMap)

	if origMap["a"] != canonMap["a"] || origMap["b"] != canonMap["b"] {
		t.Error("CanonicalizeFCIJSON should produce consistent output")
	}
}

func TestParseManifest_YAML(t *testing.T) {
	yamlData := []byte(`id: test-id
name: Test Implant
version: 1.0.0
category: utility
description: A test implant`)

	m, err := ParseManifest(yamlData)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if m.ID != "test-id" {
		t.Errorf("ID = %q, want test-id", m.ID)
	}
	if m.Name != "Test Implant" {
		t.Errorf("Name = %q, want Test Implant", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", m.Version)
	}
}

func TestParseManifest_JSON(t *testing.T) {
	jsonData := []byte(`{
  "id": "test-id",
  "name": "Test Implant",
  "version": "1.0.0",
  "category": "utility"
}`)

	m, err := ParseManifest(jsonData)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if m.ID != "test-id" {
		t.Errorf("ID = %q, want test-id", m.ID)
	}
}

func TestImplantManifest_Validate(t *testing.T) {
	validManifest := &ImplantManifest{
		ID:       "test-id",
		Name:     "Test",
		Version:  "1.0.0",
		Category: "utility",
	}

	if err := validManifest.Validate(); err != nil {
		t.Errorf("Valid manifest should pass validation: %v", err)
	}
}

func TestImplantManifest_ValidateMissingID(t *testing.T) {
	m := &ImplantManifest{
		Name:     "Test",
		Version:  "1.0.0",
		Category: "utility",
	}

	if err := m.Validate(); err == nil {
		t.Error("Missing ID should fail validation")
	}
}

func TestImplantManifest_ValidateMissingName(t *testing.T) {
	m := &ImplantManifest{
		ID:       "test-id",
		Version:  "1.0.0",
		Category: "utility",
	}

	if err := m.Validate(); err == nil {
		t.Error("Missing name should fail validation")
	}
}

func TestImplantManifest_ValidateMissingVersion(t *testing.T) {
	m := &ImplantManifest{
		ID:       "test-id",
		Name:     "Test",
		Category: "utility",
	}

	if err := m.Validate(); err == nil {
		t.Error("Missing version should fail validation")
	}
}

func TestImplantManifest_ValidateInvalidSemver(t *testing.T) {
	m := &ImplantManifest{
		ID:       "test-id",
		Name:     "Test",
		Version:  "not-semver",
		Category: "utility",
	}

	if err := m.Validate(); err == nil {
		t.Error("Invalid semver should fail validation")
	}
}

func TestImplantManifest_ValidateMissingCategory(t *testing.T) {
	m := &ImplantManifest{
		ID:      "test-id",
		Name:    "Test",
		Version: "1.0.0",
	}

	if err := m.Validate(); err == nil {
		t.Error("Missing category should fail validation")
	}
}

func TestImplantManifest_ValidateCertifiesWithoutActions(t *testing.T) {
	m := &ImplantManifest{
		ID:        "test-id",
		Name:      "Test",
		Version:   "1.0.0",
		Category:  "utility",
		Certifies: []string{"some-action"},
	}

	if err := m.Validate(); err == nil {
		t.Error("Certifies without actions should fail validation")
	}
}

func TestImplantManifest_ValidateCertifiesUnknownAction(t *testing.T) {
	m := &ImplantManifest{
		ID:        "test-id",
		Name:      "Test",
		Version:   "1.0.0",
		Category:  "utility",
		Actions:   []ActionSpec{{Name: "action1"}},
		Certifies: []string{"unknown-action"},
	}

	if err := m.Validate(); err == nil {
		t.Error("Certifies unknown action should fail validation")
	}
}

func TestImplantManifest_ValidateEmptyActionName(t *testing.T) {
	m := &ImplantManifest{
		ID:       "test-id",
		Name:     "Test",
		Version:  "1.0.0",
		Category: "utility",
		Actions:  []ActionSpec{{Name: ""}},
	}

	if err := m.Validate(); err == nil {
		t.Error("Empty action name should fail validation")
	}
}

func TestValidateSemver(t *testing.T) {
	valid := []string{"1.0.0", "0.0.1", "1.2.3-alpha", "1.2.3-alpha.1", "2.0.0"}
	for _, v := range valid {
		if err := ValidateSemver(v); err != nil {
			t.Errorf("ValidateSemver(%q) failed: %v", v, err)
		}
	}

	invalid := []string{"1", "1.0", "v1.0.0", "1.0.0.0", "latest", ""}
	for _, v := range invalid {
		if err := ValidateSemver(v); err == nil {
			t.Errorf("ValidateSemver(%q) should fail", v)
		}
	}
}

func TestImplantManifest_ToYAML(t *testing.T) {
	m := &ImplantManifest{
		ID:       "test-id",
		Name:     "Test",
		Version:  "1.0.0",
		Category: "utility",
	}

	yamlData, err := m.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	if len(yamlData) == 0 {
		t.Error("ToYAML should return non-empty data")
	}
}

func TestImplantManifest_ToJSON(t *testing.T) {
	m := &ImplantManifest{
		ID:       "test-id",
		Name:     "Test",
		Version:  "1.0.0",
		Category: "utility",
	}

	jsonData, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("ToJSON should return non-empty data")
	}
}
