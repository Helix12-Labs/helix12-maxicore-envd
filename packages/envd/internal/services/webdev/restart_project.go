package webdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// RestartProject (re)starts the project's dev-server. The implementation uses
// a pid-file convention (Manus pattern): /run/maxicore-webdev/<project>.pid.
// If a pid is present, send SIGTERM, wait for clean exit (up to 5s), then SIGKILL
// if still alive. Then start the project's dev_command (parsed from project_config).
//
// Manus 1:1 fields:
//
//	presigned_upload_url, project_config, project_name
func (s *Service) RestartProject(
	ctx context.Context,
	req *connect.Request[runtimev1.RestartProjectRequest],
) (*connect.Response[runtimev1.RestartProjectResponse], error) {
	in := req.Msg
	if in.GetProjectName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_name is required"))
	}

	ctx, cancel := withCheckpointTimeout(ctx)
	defer cancel()

	unlock := s.lockProject(in.GetProjectName())
	defer unlock()

	projectPath, err := s.projectPath(in.GetProjectName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	exists, err := s.projectExists(in.GetProjectName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("%w: %s", ErrProjectNotFound, in.GetProjectName()))
	}

	var cfg projectConfig
	if in.GetProjectConfig() != "" {
		if err := json.Unmarshal([]byte(in.GetProjectConfig()), &cfg); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid project_config json: %w", err))
		}
	}
	devCmd := strings.TrimSpace(cfg.DevCommand)
	if devCmd == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("project_config.dev_command is required"))
	}

	pidFile := pidFilePath(in.GetProjectName())

	// 1. Kill previous instance if pid-file exists
	killedPID, killMethod, killErr := stopByPIDFile(pidFile, 5*time.Second)
	if killErr != nil {
		s.logger.Warn().Err(killErr).Str("project", in.GetProjectName()).Msg("Restart: stopByPIDFile error (continuing)")
	}

	// 2. Start new dev-server (detached subprocess)
	newPID, startErr := startDetached(projectPath, devCmd, cfg.Port)
	if startErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("start dev-server: %w", startErr))
	}

	// 3. Write pid-file
	if err := writePIDFile(pidFile, newPID); err != nil {
		// don't fail the RPC for pid-file write error; the process is running
		s.logger.Warn().Err(err).Int("pid", newPID).Msg("Restart: writePIDFile failed (process still running)")
	}

	result := map[string]any{
		"status":           "ok",
		"project_name":     in.GetProjectName(),
		"new_pid":          newPID,
		"prev_pid":         killedPID,
		"kill_method":      killMethod,
		"dev_command":      devCmd,
		"port":             cfg.Port,
		"presigned_upload": in.GetPresignedUploadUrl() != "",
	}
	data, _ := json.Marshal(result)

	s.logger.Info().
		Str("project", in.GetProjectName()).
		Int("new_pid", newPID).
		Int("prev_pid", killedPID).
		Str("kill_method", killMethod).
		Msg("RestartProject completed")

	return connect.NewResponse(&runtimev1.RestartProjectResponse{Data: string(data)}), nil
}

// pidFilePath returns the canonical pid-file path for a project.
func pidFilePath(projectName string) string {
	return filepath.Join("/run/maxicore-webdev", sanitize(projectName)+".pid")
}

// stopByPIDFile reads pidFile, sends SIGTERM, waits up to grace, then SIGKILL.
// Returns the killed PID (0 if no pid-file), the method used ("none" | "sigterm" | "sigkill"),
// and any error encountered while killing.
func stopByPIDFile(pidFile string, grace time.Duration) (int, string, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "none", nil
		}
		return 0, "none", err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, "none", fmt.Errorf("invalid pid in %s: %w", pidFile, err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, "none", err
	}

	// Probe alive (signal 0)
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// already dead
		_ = os.Remove(pidFile)
		return pid, "none", nil
	}

	// SIGTERM
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return pid, "none", err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			_ = os.Remove(pidFile)
			return pid, "sigterm", nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// SIGKILL
	_ = proc.Kill()
	_ = os.Remove(pidFile)
	return pid, "sigkill", nil
}

// startDetached starts the dev-command as a detached subprocess in projectPath.
// Returns the new PID. The subprocess inherits no stdin; stdout/stderr go to
// /run/maxicore-webdev/<project>.log.
func startDetached(projectPath, devCmd, port string) (int, error) {
	// Resolve a shell to invoke the dev_command. We use `sh -c` because
	// dev_command is user-controlled and may be a shell expression (e.g. "PORT=3000 pnpm dev").
	shell := "/bin/sh"

	if err := os.MkdirAll("/run/maxicore-webdev", 0o755); err != nil {
		return 0, fmt.Errorf("mkdir run-dir: %w", err)
	}

	logFile := filepath.Join("/run/maxicore-webdev", sanitize(filepath.Base(projectPath))+".log")
	logFD, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log: %w", err)
	}
	defer logFD.Close()

	env := append([]string(nil), os.Environ()...)
	if port != "" {
		env = append(env, "PORT="+port)
	}

	cmd := exec.Command(shell, "-c", devCmd)
	cmd.Dir = projectPath
	cmd.Env = env
	cmd.Stdout = logFD
	cmd.Stderr = logFD
	// Detach: new process-group, no parent waits
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// Reap async: parent does NOT wait — but to avoid zombies we
	// detach via Process.Release after Start (Release lets the child be
	// inherited by init upon parent exit).
	if err := cmd.Process.Release(); err != nil {
		return cmd.Process.Pid, fmt.Errorf("release child: %w", err)
	}
	return cmd.Process.Pid, nil
}

// writePIDFile atomically writes pid to pidFile.
func writePIDFile(pidFile string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		return err
	}
	tmp := pidFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, pidFile)
}
