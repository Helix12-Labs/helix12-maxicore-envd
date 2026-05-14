// Package handlers wires the runtime.v1.* Connect-RPC services to their
// in-VM implementations. Where an implementation does not yet exist, the
// handler returns connect.CodeUnimplemented with a stable message so that
// clients can detect "not yet implemented" cleanly.
//
// WebdevServiceHandler delegates the 4 implemented RPCs to the webdev.Service
// from packages/envd/internal/services/webdev. The remaining 10 RPCs return
// CodeUnimplemented; they will be filled in by per-RPC follow-up sprints
// (RefreshGitHubToken, RefreshS3Token, InitProject, UpdateConfig, etc.).
package handlers

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/webdev"
)

// WebdevHandler implements runtimev1connect.WebdevServiceHandler.
//
// Implemented RPCs (delegated to webdev.Service):
//   - SaveCheckpoint
//   - RestoreProject
//   - RollbackProject
//   - RestartProject
//
// Unimplemented RPCs (return CodeUnimplemented):
//   - InitProject, UpdateConfig, GitSyncCheck, ApplyWebpageEdits,
//     DatabaseOperation, RefreshGitHubToken, RefreshS3Token,
//     GetProjectStatus, GitPush, SetupImportedTemplate
type WebdevHandler struct {
	svc *webdev.Service
}

// NewWebdevHandler builds a handler around an existing webdev.Service.
func NewWebdevHandler(svc *webdev.Service) (*WebdevHandler, error) {
	if svc == nil {
		return nil, errors.New("NewWebdevHandler: svc is required")
	}
	return &WebdevHandler{svc: svc}, nil
}

// --- Implemented RPCs (delegate to webdev.Service) ---

func (h *WebdevHandler) SaveCheckpoint(
	ctx context.Context,
	req *connect.Request[runtimev1.SaveCheckpointRequest],
) (*connect.Response[runtimev1.SaveCheckpointResponse], error) {
	return h.svc.SaveCheckpoint(ctx, req)
}

func (h *WebdevHandler) RestoreProject(
	ctx context.Context,
	req *connect.Request[runtimev1.RestoreProjectRequest],
) (*connect.Response[runtimev1.RestoreProjectResponse], error) {
	return h.svc.RestoreProject(ctx, req)
}

func (h *WebdevHandler) RollbackProject(
	ctx context.Context,
	req *connect.Request[runtimev1.RollbackProjectRequest],
) (*connect.Response[runtimev1.RollbackProjectResponse], error) {
	return h.svc.RollbackProject(ctx, req)
}

func (h *WebdevHandler) RestartProject(
	ctx context.Context,
	req *connect.Request[runtimev1.RestartProjectRequest],
) (*connect.Response[runtimev1.RestartProjectResponse], error) {
	return h.svc.RestartProject(ctx, req)
}

// --- Unimplemented RPCs (follow-up sprints per RPC) ---

func (h *WebdevHandler) InitProject(
	ctx context.Context,
	req *connect.Request[runtimev1.InitProjectRequest],
) (*connect.Response[runtimev1.InitProjectResponse], error) {
	return nil, unimpl("WebdevService.InitProject", "B.II.x.WebDev-InitProject")
}

func (h *WebdevHandler) UpdateConfig(
	ctx context.Context,
	req *connect.Request[runtimev1.UpdateConfigRequest],
) (*connect.Response[runtimev1.UpdateConfigResponse], error) {
	return nil, unimpl("WebdevService.UpdateConfig", "B.II.x.WebDev-UpdateConfig")
}

func (h *WebdevHandler) GitSyncCheck(
	ctx context.Context,
	req *connect.Request[runtimev1.GitSyncCheckRequest],
) (*connect.Response[runtimev1.GitSyncCheckResponse], error) {
	return nil, unimpl("WebdevService.GitSyncCheck", "B.II.x.WebDev-Git")
}

func (h *WebdevHandler) ApplyWebpageEdits(
	ctx context.Context,
	req *connect.Request[runtimev1.ApplyWebpageEditsRequest],
) (*connect.Response[runtimev1.ApplyWebpageEditsResponse], error) {
	return nil, unimpl("WebdevService.ApplyWebpageEdits", "B.II.x.WebDev-Edits")
}

func (h *WebdevHandler) DatabaseOperation(
	ctx context.Context,
	req *connect.Request[runtimev1.DatabaseOperationRequest],
) (*connect.Response[runtimev1.DatabaseOperationResponse], error) {
	return nil, unimpl("WebdevService.DatabaseOperation", "B.II.x.WebDev-DB")
}

func (h *WebdevHandler) RefreshGitHubToken(
	ctx context.Context,
	req *connect.Request[runtimev1.RefreshGitHubTokenRequest],
) (*connect.Response[runtimev1.RefreshGitHubTokenResponse], error) {
	return nil, unimpl("WebdevService.RefreshGitHubToken", "B.II.x.WebDev-Tokens")
}

func (h *WebdevHandler) RefreshS3Token(
	ctx context.Context,
	req *connect.Request[runtimev1.RefreshS3TokenRequest],
) (*connect.Response[runtimev1.RefreshS3TokenResponse], error) {
	return nil, unimpl("WebdevService.RefreshS3Token", "B.II.x.WebDev-Tokens")
}

func (h *WebdevHandler) GetProjectStatus(
	ctx context.Context,
	req *connect.Request[runtimev1.GetProjectStatusRequest],
) (*connect.Response[runtimev1.GetProjectStatusResponse], error) {
	return nil, unimpl("WebdevService.GetProjectStatus", "B.II.x.WebDev-Status")
}

func (h *WebdevHandler) GitPush(
	ctx context.Context,
	req *connect.Request[runtimev1.GitPushRequest],
) (*connect.Response[runtimev1.GitPushResponse], error) {
	return nil, unimpl("WebdevService.GitPush", "B.II.x.WebDev-Git")
}

func (h *WebdevHandler) SetupImportedTemplate(
	ctx context.Context,
	req *connect.Request[runtimev1.SetupImportedTemplateRequest],
) (*connect.Response[runtimev1.SetupImportedTemplateResponse], error) {
	return nil, unimpl("WebdevService.SetupImportedTemplate", "B.II.x.WebDev-Templates")
}

// unimpl returns a stable CodeUnimplemented error with the RPC name and the
// tracking sprint-tag so callers can grep server logs to find blocking work.
func unimpl(rpc, sprint string) error {
	return connect.NewError(
		connect.CodeUnimplemented,
		fmt.Errorf("%s not yet implemented; tracked by sprint %s", rpc, sprint),
	)
}
