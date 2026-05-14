package handlers

import (
	"context"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// This file contains stub-handlers for the 10 services whose business-logic
// has not yet been ported from manus-sandbox. Each method returns
// connect.CodeUnimplemented with the RPC name + tracking sprint-tag.
//
// The stubs let envd register a full runtime.v1 surface so clients can
// discover service routes; they receive a clear 501-equivalent that names
// the missing sprint, instead of HTTP 404.
//
// Implementing each handler is a future-sprint per service (B.II.x.*).

// ---------------------------------------------------------------------------
// BrowserService (5 RPCs)
// ---------------------------------------------------------------------------

type BrowserStubHandler struct{}

func (BrowserStubHandler) ExecuteBrowserAction(
	ctx context.Context,
	req *connect.Request[runtimev1.ExecuteBrowserActionRequest],
) (*connect.Response[runtimev1.ExecuteBrowserActionResponse], error) {
	return nil, unimpl("BrowserService.ExecuteBrowserAction", "B.II.x.BrowserService")
}

func (BrowserStubHandler) GetBrowserStatus(
	ctx context.Context,
	req *connect.Request[runtimev1.GetBrowserStatusRequest],
) (*connect.Response[runtimev1.GetBrowserStatusResponse], error) {
	return nil, unimpl("BrowserService.GetBrowserStatus", "B.II.x.BrowserService")
}

func (BrowserStubHandler) ClearBrowserData(
	ctx context.Context,
	req *connect.Request[runtimev1.ClearBrowserDataRequest],
) (*connect.Response[runtimev1.ClearBrowserDataResponse], error) {
	return nil, unimpl("BrowserService.ClearBrowserData", "B.II.x.BrowserService")
}

func (BrowserStubHandler) NotifyBrowserConfigUpdate(
	ctx context.Context,
	req *connect.Request[runtimev1.NotifyBrowserConfigUpdateRequest],
) (*connect.Response[runtimev1.NotifyBrowserConfigUpdateResponse], error) {
	return nil, unimpl("BrowserService.NotifyBrowserConfigUpdate", "B.II.x.BrowserService")
}

func (BrowserStubHandler) RenewPresignedURLs(
	ctx context.Context,
	req *connect.Request[runtimev1.RenewPresignedURLsRequest],
) (*connect.Response[runtimev1.RenewPresignedURLsResponse], error) {
	return nil, unimpl("BrowserService.RenewPresignedURLs", "B.II.x.BrowserService")
}

// ---------------------------------------------------------------------------
// ConfigService (6 RPCs)
// ---------------------------------------------------------------------------

type ConfigStubHandler struct{}

func (ConfigStubHandler) Mount(
	ctx context.Context,
	req *connect.Request[runtimev1.MountRequest],
) (*connect.Response[runtimev1.MountResponse], error) {
	return nil, unimpl("ConfigService.Mount", "B.II.x.ConfigService-Mount")
}

func (ConfigStubHandler) GetGitHubAuthStatus(
	ctx context.Context,
	req *connect.Request[runtimev1.GetGitHubAuthStatusRequest],
) (*connect.Response[runtimev1.GetGitHubAuthStatusResponse], error) {
	return nil, unimpl("ConfigService.GetGitHubAuthStatus", "B.II.x.ConfigService-Auth")
}

func (ConfigStubHandler) NekoPreAuth(
	ctx context.Context,
	req *connect.Request[runtimev1.NekoPreAuthRequest],
) (*connect.Response[runtimev1.NekoPreAuthResponse], error) {
	return nil, unimpl("ConfigService.NekoPreAuth", "B.II.x.ConfigService-VNC")
}

func (ConfigStubHandler) RefreshTurnToken(
	ctx context.Context,
	req *connect.Request[runtimev1.RefreshTurnTokenRequest],
) (*connect.Response[runtimev1.RefreshTurnTokenResponse], error) {
	return nil, unimpl("ConfigService.RefreshTurnToken", "B.II.x.ConfigService-TURN")
}

// SetEnv is referenced in protos but the message types are stubs; will be
// filled when the spec is finalised.
func (ConfigStubHandler) SetEnv(
	ctx context.Context,
	req *connect.Request[runtimev1.SetEnvRequest],
) (*connect.Response[runtimev1.SetEnvResponse], error) {
	return nil, unimpl("ConfigService.SetEnv", "B.II.x.ConfigService-Env")
}

func (ConfigStubHandler) VNCPreAuth(
	ctx context.Context,
	req *connect.Request[runtimev1.VNCPreAuthRequest],
) (*connect.Response[runtimev1.VNCPreAuthResponse], error) {
	return nil, unimpl("ConfigService.VNCPreAuth", "B.II.x.ConfigService-VNC")
}

// ---------------------------------------------------------------------------
// TerminalService (5 RPCs)
// ---------------------------------------------------------------------------

type TerminalStubHandler struct{}

func (TerminalStubHandler) View(
	ctx context.Context,
	req *connect.Request[runtimev1.ViewRequest],
) (*connect.Response[runtimev1.ViewResponse], error) {
	return nil, unimpl("TerminalService.View", "B.II.x.TerminalService")
}

func (TerminalStubHandler) Write(
	ctx context.Context,
	req *connect.Request[runtimev1.WriteRequest],
) (*connect.Response[runtimev1.WriteResponse], error) {
	return nil, unimpl("TerminalService.Write", "B.II.x.TerminalService")
}

func (TerminalStubHandler) Reset(
	ctx context.Context,
	req *connect.Request[runtimev1.ResetRequest],
) (*connect.Response[runtimev1.ResetResponse], error) {
	return nil, unimpl("TerminalService.Reset", "B.II.x.TerminalService")
}

func (TerminalStubHandler) ResetAll(
	ctx context.Context,
	req *connect.Request[runtimev1.ResetAllRequest],
) (*connect.Response[runtimev1.ResetAllResponse], error) {
	return nil, unimpl("TerminalService.ResetAll", "B.II.x.TerminalService")
}

func (TerminalStubHandler) KillTerminalProcess(
	ctx context.Context,
	req *connect.Request[runtimev1.KillTerminalProcessRequest],
) (*connect.Response[runtimev1.KillTerminalProcessResponse], error) {
	return nil, unimpl("TerminalService.KillTerminalProcess", "B.II.x.TerminalService")
}

// ---------------------------------------------------------------------------
// FileService (11 RPCs)
// ---------------------------------------------------------------------------

type FileStubHandler struct{}

func (FileStubHandler) DownloadFile(
	ctx context.Context,
	req *connect.Request[runtimev1.DownloadFileRequest],
) (*connect.Response[runtimev1.DownloadFileResponse], error) {
	return nil, unimpl("FileService.DownloadFile", "B.II.x.FileService-Read")
}

func (FileStubHandler) BatchReadFiles(
	ctx context.Context,
	req *connect.Request[runtimev1.BatchReadFilesRequest],
) (*connect.Response[runtimev1.BatchReadFilesResponse], error) {
	return nil, unimpl("FileService.BatchReadFiles", "B.II.x.FileService-Read")
}

func (FileStubHandler) CheckFilesExist(
	ctx context.Context,
	req *connect.Request[runtimev1.CheckFilesExistRequest],
) (*connect.Response[runtimev1.CheckFilesExistResponse], error) {
	return nil, unimpl("FileService.CheckFilesExist", "B.II.x.FileService-Stat")
}

func (FileStubHandler) CheckFilesExistAbs(
	ctx context.Context,
	req *connect.Request[runtimev1.CheckFilesExistAbsRequest],
) (*connect.Response[runtimev1.CheckFilesExistAbsResponse], error) {
	return nil, unimpl("FileService.CheckFilesExistAbs", "B.II.x.FileService-Stat")
}

func (FileStubHandler) MultipartUpload(
	ctx context.Context,
	req *connect.Request[runtimev1.MultipartUploadRequest],
) (*connect.Response[runtimev1.MultipartUploadResponse], error) {
	return nil, unimpl("FileService.MultipartUpload", "B.II.x.FileService-Upload")
}

func (FileStubHandler) UploadFileToS3(
	ctx context.Context,
	req *connect.Request[runtimev1.UploadFileToS3Request],
) (*connect.Response[runtimev1.UploadFileToS3Response], error) {
	return nil, unimpl("FileService.UploadFileToS3", "B.II.x.FileService-Upload")
}

func (FileStubHandler) UploadFilesToS3(
	ctx context.Context,
	req *connect.Request[runtimev1.UploadFilesToS3Request],
) (*connect.Response[runtimev1.UploadFilesToS3Response], error) {
	return nil, unimpl("FileService.UploadFilesToS3", "B.II.x.FileService-Upload")
}

func (FileStubHandler) UploadSlideTemplateImage(
	ctx context.Context,
	req *connect.Request[runtimev1.UploadSlideTemplateImageRequest],
) (*connect.Response[runtimev1.UploadSlideTemplateImageResponse], error) {
	return nil, unimpl("FileService.UploadSlideTemplateImage", "B.II.x.FileService-Upload")
}

func (FileStubHandler) StoreParallelTasksResult(
	ctx context.Context,
	req *connect.Request[runtimev1.StoreParallelTasksResultRequest],
) (*connect.Response[runtimev1.StoreParallelTasksResultResponse], error) {
	return nil, unimpl("FileService.StoreParallelTasksResult", "B.II.x.FileService-Tasks")
}

func (FileStubHandler) DownloadAttachments(
	ctx context.Context,
	req *connect.Request[runtimev1.DownloadAttachmentsRequest],
) (*connect.Response[runtimev1.DownloadAttachmentsResponse], error) {
	return nil, unimpl("FileService.DownloadAttachments", "B.II.x.FileService-Attach")
}

func (FileStubHandler) DownloadUserEdited(
	ctx context.Context,
	req *connect.Request[runtimev1.DownloadUserEditedRequest],
) (*connect.Response[runtimev1.DownloadUserEditedResponse], error) {
	return nil, unimpl("FileService.DownloadUserEdited", "B.II.x.FileService-Attach")
}

// ---------------------------------------------------------------------------
// SlideService (7 RPCs)
// ---------------------------------------------------------------------------

type SlideStubHandler struct{}

func (SlideStubHandler) InitSlideProject(
	ctx context.Context,
	req *connect.Request[runtimev1.InitSlideProjectRequest],
) (*connect.Response[runtimev1.InitSlideProjectResponse], error) {
	return nil, unimpl("SlideService.InitSlideProject", "B.II.x.SlideService")
}

func (SlideStubHandler) ModifySlide(
	ctx context.Context,
	req *connect.Request[runtimev1.ModifySlideRequest],
) (*connect.Response[runtimev1.ModifySlideResponse], error) {
	return nil, unimpl("SlideService.ModifySlide", "B.II.x.SlideService")
}

func (SlideStubHandler) PresentSlide(
	ctx context.Context,
	req *connect.Request[runtimev1.PresentSlideRequest],
) (*connect.Response[runtimev1.PresentSlideResponse], error) {
	return nil, unimpl("SlideService.PresentSlide", "B.II.x.SlideService")
}

func (SlideStubHandler) FetchSlideNotes(
	ctx context.Context,
	req *connect.Request[runtimev1.FetchSlideNotesRequest],
) (*connect.Response[runtimev1.FetchSlideNotesResponse], error) {
	return nil, unimpl("SlideService.FetchSlideNotes", "B.II.x.SlideService")
}

func (SlideStubHandler) UpdateSlide(
	ctx context.Context,
	req *connect.Request[runtimev1.UpdateSlideRequest],
) (*connect.Response[runtimev1.UpdateSlideResponse], error) {
	return nil, unimpl("SlideService.UpdateSlide", "B.II.x.SlideService")
}

func (SlideStubHandler) UploadSlide(
	ctx context.Context,
	req *connect.Request[runtimev1.UploadSlideRequest],
) (*connect.Response[runtimev1.UploadSlideResponse], error) {
	return nil, unimpl("SlideService.UploadSlide", "B.II.x.SlideService")
}

func (SlideStubHandler) UploadSlideTemplate(
	ctx context.Context,
	req *connect.Request[runtimev1.UploadSlideTemplateRequest],
) (*connect.Response[runtimev1.UploadSlideTemplateResponse], error) {
	return nil, unimpl("SlideService.UploadSlideTemplate", "B.II.x.SlideService")
}

// ---------------------------------------------------------------------------
// DeployService (3 RPCs)
// ---------------------------------------------------------------------------

type DeployStubHandler struct{}

func (DeployStubHandler) CreateWebdevProject(
	ctx context.Context,
	req *connect.Request[runtimev1.CreateWebdevProjectRequest],
) (*connect.Response[runtimev1.CreateWebdevProjectResponse], error) {
	return nil, unimpl("DeployService.CreateWebdevProject", "B.II.x.DeployService")
}

func (DeployStubHandler) ZipAndUpload(
	ctx context.Context,
	req *connect.Request[runtimev1.ZipAndUploadRequest],
) (*connect.Response[runtimev1.ZipAndUploadResponse], error) {
	return nil, unimpl("DeployService.ZipAndUpload", "B.II.x.DeployService")
}

func (DeployStubHandler) SwitchVersion(
	ctx context.Context,
	req *connect.Request[runtimev1.SwitchVersionRequest],
) (*connect.Response[runtimev1.SwitchVersionResponse], error) {
	return nil, unimpl("DeployService.SwitchVersion", "B.II.x.DeployService")
}

// ---------------------------------------------------------------------------
// EmailService (4 RPCs)
// ---------------------------------------------------------------------------

type EmailStubHandler struct{}

func (EmailStubHandler) DownloadGmailAttachments(
	ctx context.Context,
	req *connect.Request[runtimev1.DownloadGmailAttachmentsRequest],
) (*connect.Response[runtimev1.DownloadGmailAttachmentsResponse], error) {
	return nil, unimpl("EmailService.DownloadGmailAttachments", "B.II.x.EmailService")
}

func (EmailStubHandler) DownloadOutlookAttachments(
	ctx context.Context,
	req *connect.Request[runtimev1.DownloadOutlookAttachmentsRequest],
) (*connect.Response[runtimev1.DownloadOutlookAttachmentsResponse], error) {
	return nil, unimpl("EmailService.DownloadOutlookAttachments", "B.II.x.EmailService")
}

func (EmailStubHandler) SendOrSaveGmailDraft(
	ctx context.Context,
	req *connect.Request[runtimev1.SendOrSaveGmailDraftRequest],
) (*connect.Response[runtimev1.SendOrSaveGmailDraftResponse], error) {
	return nil, unimpl("EmailService.SendOrSaveGmailDraft", "B.II.x.EmailService")
}

func (EmailStubHandler) SendOrSaveOutlookDraft(
	ctx context.Context,
	req *connect.Request[runtimev1.SendOrSaveOutlookDraftRequest],
) (*connect.Response[runtimev1.SendOrSaveOutlookDraftResponse], error) {
	return nil, unimpl("EmailService.SendOrSaveOutlookDraft", "B.II.x.EmailService")
}

// ---------------------------------------------------------------------------
// SkillService (2 RPCs)
// ---------------------------------------------------------------------------

type SkillStubHandler struct{}

func (SkillStubHandler) PackageSkill(
	ctx context.Context,
	req *connect.Request[runtimev1.PackageSkillRequest],
) (*connect.Response[runtimev1.PackageSkillResponse], error) {
	return nil, unimpl("SkillService.PackageSkill", "B.II.x.SkillService")
}

func (SkillStubHandler) SyncSkills(
	ctx context.Context,
	req *connect.Request[runtimev1.SyncSkillsRequest],
) (*connect.Response[runtimev1.SyncSkillsResponse], error) {
	return nil, unimpl("SkillService.SyncSkills", "B.II.x.SkillService")
}

// ---------------------------------------------------------------------------
// ImageService (2 RPCs)
// ---------------------------------------------------------------------------

type ImageStubHandler struct{}

func (ImageStubHandler) RemoveBackground(
	ctx context.Context,
	req *connect.Request[runtimev1.RemoveBackgroundRequest],
) (*connect.Response[runtimev1.RemoveBackgroundResponse], error) {
	return nil, unimpl("ImageService.RemoveBackground", "B.II.x.ImageService")
}

func (ImageStubHandler) UploadSearchImage(
	ctx context.Context,
	req *connect.Request[runtimev1.UploadSearchImageRequest],
) (*connect.Response[runtimev1.UploadSearchImageResponse], error) {
	return nil, unimpl("ImageService.UploadSearchImage", "B.II.x.ImageService")
}

// ---------------------------------------------------------------------------
// TextEditorService (1 RPC)
// ---------------------------------------------------------------------------

type TextEditorStubHandler struct{}

func (TextEditorStubHandler) RunTextEditor(
	ctx context.Context,
	req *connect.Request[runtimev1.RunTextEditorRequest],
) (*connect.Response[runtimev1.RunTextEditorResponse], error) {
	return nil, unimpl("TextEditorService.RunTextEditor", "B.II.x.TextEditorService")
}
