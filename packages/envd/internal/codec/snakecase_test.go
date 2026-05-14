package codec

import (
	"bytes"
	"strings"
	"testing"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

func newCodec() *SnakeCaseJSONCodec {
	return &SnakeCaseJSONCodec{}
}

// TC1: codec name must be "json" for Connect-RPC to register it under application/json.
func TestSnakeCaseJSONCodec_Name(t *testing.T) {
	if got := newCodec().Name(); got != "json" {
		t.Fatalf("expected 'json', got %q", got)
	}
}

// TC2: Marshal a SaveCheckpointRequest → JSON must use snake_case keys.
// Manus's wire-format requires checkpoint_zip_upload_url (NOT checkpointZipUploadUrl).
func TestSnakeCaseJSONCodec_Marshal_SaveCheckpoint_SnakeCase(t *testing.T) {
	req := &runtimev1.SaveCheckpointRequest{
		ProjectName:            "my-project",
		ProjectConfig:          "{}",
		Description:            "checkpoint v1",
		LastCheckpointCommit:   "abc123",
		CheckpointZipUploadUrl: "https://s3/upload",
		CheckpointZipUrl:       "https://s3/get",
		Timeout:                "300",
	}
	data, err := newCodec().Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	json := string(data)

	// MUST contain snake_case
	expectedSnake := []string{
		"project_name",
		"project_config",
		"last_checkpoint_commit",
		"checkpoint_zip_upload_url",
		"checkpoint_zip_url",
	}
	for _, key := range expectedSnake {
		if !strings.Contains(json, key) {
			t.Fatalf("expected snake_case %q in JSON, got: %s", key, json)
		}
	}

	// MUST NOT contain camelCase (Manus wire-format rejects camelCase)
	forbiddenCamel := []string{
		"projectName",
		"projectConfig",
		"lastCheckpointCommit",
		"checkpointZipUploadUrl",
		"checkpointZipUrl",
	}
	for _, key := range forbiddenCamel {
		if strings.Contains(json, key) {
			t.Fatalf("unexpected camelCase %q in JSON, got: %s", key, json)
		}
	}
}

// TC3: Unmarshal snake_case JSON → struct fields populated correctly.
func TestSnakeCaseJSONCodec_Unmarshal_SnakeCase(t *testing.T) {
	input := []byte(`{
		"project_name": "p1",
		"checkpoint_zip_upload_url": "https://s3/u",
		"last_checkpoint_commit": "sha1"
	}`)
	var req runtimev1.SaveCheckpointRequest
	if err := newCodec().Unmarshal(input, &req); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if req.ProjectName != "p1" {
		t.Errorf("project_name not parsed: %q", req.ProjectName)
	}
	if req.CheckpointZipUploadUrl != "https://s3/u" {
		t.Errorf("checkpoint_zip_upload_url not parsed: %q", req.CheckpointZipUploadUrl)
	}
	if req.LastCheckpointCommit != "sha1" {
		t.Errorf("last_checkpoint_commit not parsed: %q", req.LastCheckpointCommit)
	}
}

// TC4: Roundtrip — Marshal then Unmarshal must yield equivalent message.
func TestSnakeCaseJSONCodec_Roundtrip(t *testing.T) {
	original := &runtimev1.SaveCheckpointRequest{
		ProjectName:          "round-trip",
		Description:          "test description with spaces",
		LastCheckpointCommit: "deadbeef",
	}
	data, err := newCodec().Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded runtimev1.SaveCheckpointRequest
	if err := newCodec().Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ProjectName != original.ProjectName {
		t.Errorf("ProjectName mismatch: got %q want %q", decoded.ProjectName, original.ProjectName)
	}
	if decoded.Description != original.Description {
		t.Errorf("Description mismatch: got %q want %q", decoded.Description, original.Description)
	}
	if decoded.LastCheckpointCommit != original.LastCheckpointCommit {
		t.Errorf("LastCheckpointCommit mismatch: got %q want %q", decoded.LastCheckpointCommit, original.LastCheckpointCommit)
	}
}

// TC5: DiscardUnknown=true — codec must tolerate unknown future fields.
func TestSnakeCaseJSONCodec_Unmarshal_UnknownField_Tolerated(t *testing.T) {
	input := []byte(`{
		"project_name": "p1",
		"future_field_not_yet_in_proto": "ignored",
		"another_unknown": 42
	}`)
	var req runtimev1.SaveCheckpointRequest
	if err := newCodec().Unmarshal(input, &req); err != nil {
		t.Fatalf("expected DiscardUnknown to tolerate, got error: %v", err)
	}
	if req.ProjectName != "p1" {
		t.Errorf("known field not parsed despite unknown fields: %q", req.ProjectName)
	}
}

// TC6: Non-proto.Message input must be rejected.
func TestSnakeCaseJSONCodec_NonProtoMessage_Rejected(t *testing.T) {
	c := newCodec()
	_, err := c.Marshal(struct{ X int }{X: 1})
	if err == nil {
		t.Fatal("expected error for non-proto.Message marshal")
	}
	err = c.Unmarshal([]byte(`{}`), &struct{ X int }{})
	if err == nil {
		t.Fatal("expected error for non-proto.Message unmarshal")
	}
}

// TC7: nil input must be rejected with clear error.
func TestSnakeCaseJSONCodec_Nil_Rejected(t *testing.T) {
	c := newCodec()
	if _, err := c.Marshal(nil); err == nil {
		t.Fatal("expected error for nil marshal")
	}
	if err := c.Unmarshal([]byte(`{}`), nil); err == nil {
		t.Fatal("expected error for nil unmarshal")
	}
}

// TC8: GetHealthResponse (empty-ish message) round-trips.
func TestSnakeCaseJSONCodec_GetHealthResponse_Roundtrip(t *testing.T) {
	orig := &runtimev1.GetHealthResponse{Status: "ok"}
	data, err := newCodec().Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"status":"ok"`)) {
		t.Fatalf("expected snake_case 'status' in: %s", data)
	}
	var decoded runtimev1.GetHealthResponse
	if err := newCodec().Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != "ok" {
		t.Fatalf("status mismatch: got %q", decoded.Status)
	}
}

// TC9: WebdevService RestoreProjectRequest with 5 fields (verified Manus-spec).
func TestSnakeCaseJSONCodec_RestoreProjectRequest_5Fields(t *testing.T) {
	req := &runtimev1.RestoreProjectRequest{
		Capabilities:  "{\"db\":true}",
		Experiments:   "[]",
		Platform:      "linux",
		ProjectConfig: "{}",
		ProjectName:   "p",
	}
	data, err := newCodec().Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{"capabilities", "experiments", "platform", "project_config", "project_name"}
	for _, k := range required {
		if !bytes.Contains(data, []byte(`"`+k+`"`)) {
			t.Errorf("expected snake_case %q in JSON, got: %s", k, data)
		}
	}
}
