package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"connectrpc.com/authn"
	connectcors "connectrpc.com/cors"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
	"github.com/rs/zerolog"

	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/api"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/auth"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/execcontext"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/host"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/logs"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/permissions"
	publicport "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/port"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/cgroups"
	filesystemRpc "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/filesystem"
	processRpc "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/process"
	runtimeRpc "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime"
	processSpec "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/spec/process"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/webdev"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/utils"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/pkg"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/shared/pkg/httpserver"
)

const (
	// Downstream timeout should be greater than upstream (in orchestrator proxy).
	idleTimeout = 640 * time.Second
	maxAge      = 2 * time.Hour

	defaultPort = 49983

	portScannerInterval = 1000 * time.Millisecond

	// This is the default user used in the container if not specified otherwise.
	// It should be always overridden by the user in /init when building the template.
	defaultUser = "root"

	kilobyte = 1024
	megabyte = 1024 * kilobyte
)

var (
	commitSHA string

	isNotFC bool
	port    int64

	versionFlag  bool
	commitFlag   bool
	startCmdFlag string
	cgroupRoot   string
)

func parseFlags() {
	flag.BoolVar(
		&isNotFC,
		"isnotfc",
		false,
		"isNotFCmode prints all logs to stdout",
	)

	flag.BoolVar(
		&versionFlag,
		"version",
		false,
		"print envd version",
	)

	flag.BoolVar(
		&commitFlag,
		"commit",
		false,
		"print envd source commit",
	)

	flag.Int64Var(
		&port,
		"port",
		defaultPort,
		"a port on which the daemon should run",
	)

	flag.StringVar(
		&startCmdFlag,
		"cmd",
		"",
		"a command to run on the daemon start",
	)

	flag.StringVar(
		&cgroupRoot,
		"cgroup-root",
		"/sys/fs/cgroup",
		"cgroup root directory",
	)

	flag.Parse()
}

func withCORS(h http.Handler) http.Handler {
	middleware := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodHead,
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		},
		AllowedHeaders: []string{"*"},
		ExposedHeaders: append(
			connectcors.ExposedHeaders(),
			"Location",
			"Cache-Control",
			"X-Content-Type-Options",
		),
		MaxAge: int(maxAge.Seconds()),
	})

	return middleware.Handler(h)
}

func main() {
	parseFlags()

	if versionFlag {
		fmt.Printf("%s\n", pkg.Version)

		return
	}

	if commitFlag {
		fmt.Printf("%s\n", commitSHA)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := os.MkdirAll(host.E2BRunDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating E2B run directory: %v\n", err)
	}

	defaults := &execcontext.Defaults{
		User:    defaultUser,
		EnvVars: utils.NewMap[string, string](),
	}
	isFCBoolStr := strconv.FormatBool(!isNotFC)
	defaults.EnvVars.Store("E2B_SANDBOX", isFCBoolStr)
	if err := os.WriteFile(filepath.Join(host.E2BRunDir, ".E2B_SANDBOX"), []byte(isFCBoolStr), 0o444); err != nil {
		fmt.Fprintf(os.Stderr, "error writing sandbox file: %v\n", err)
	}

	mmdsChan := make(chan *host.MMDSOpts, 1)
	defer close(mmdsChan)
	if !isNotFC {
		go host.PollForMMDSOpts(ctx, mmdsChan, defaults.EnvVars)
	}

	l := logs.NewLogger(ctx, isNotFC, mmdsChan)

	m := chi.NewRouter()

	envLogger := l.With().Str("logger", "envd").Logger()
	fsLogger := l.With().Str("logger", "filesystem").Logger()
	filesystemRpc.Handle(m, &fsLogger, defaults)

	cgroupManager := createCgroupManager()
	defer func() {
		err := cgroupManager.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to close cgroup manager: %v\n", err)
		}
	}()

	processLogger := l.With().Str("logger", "process").Logger()
	processService := processRpc.Handle(m, &processLogger, defaults, cgroupManager)

	// --- B.II.1d-wire: mount runtime.v1.* Connect-RPC surface (Manus 1:1) ---
	//
	// HMAC-V2 secret resolution order:
	//   1. env MAXICORE_SANDBOX_SECRET (typically injected by sandbox-manager)
	//   2. /etc/maxicore/sandbox-secret (0600, written by initramfs)
	// If neither source provides a non-empty secret, runtime.Mount is skipped
	// with a warning — envd remains usable for e2b legacy clients but the
	// runtime.v1 RPC surface is offline. Production deployments MUST provide
	// the secret.
	runtimeLogger := l.With().Str("logger", "runtime-rpc").Logger()
	webdevSvc, err := webdev.NewService(webdev.Config{Logger: &runtimeLogger})
	if err != nil {
		runtimeLogger.Error().Err(err).Msg("webdev.NewService failed; runtime.v1 surface NOT mounted")
	} else {
		// runtime.v1 is ALWAYS mounted. The HMAC secret is resolved lazily on
		// first request via readSandboxSecret (B.II.2.B Sprint-14 forensik fix):
		// e2b TemplateCreate snapshots envd's running state during template-build
		// — a startup-only secret read froze envd in legacy-only mode after
		// resume even though /etc/maxicore/sandbox-secret was present. Lazy
		// resolution makes the rootfs file / e2b /init env take effect
		// post-resume. Requests before the secret resolves get 401, not 404.
		authMw := auth.NewMiddlewareLazy(func() []byte {
			return readSandboxSecret(&runtimeLogger)
		}, 60*time.Second)
		mounted, err := runtimeRpc.Mount(m, &runtimeRpc.Deps{
			Auth:      authMw,
			WebdevSvc: webdevSvc,
			Version:   pkg.Version,
		})
		if err != nil {
			runtimeLogger.Error().Err(err).Msg("runtime.Mount failed; runtime.v1 surface NOT mounted")
		} else {
			runtimeLogger.Info().
				Int("services", len(mounted)).
				Strs("paths", mounted).
				Msg("runtime.v1 Connect-RPC surface mounted (lazy HMAC-V2 secret)")
		}
	}

	service := api.New(&envLogger, defaults, mmdsChan, isNotFC)
	handler := api.HandlerFromMux(service, m)
	middleware := authn.NewMiddleware(permissions.AuthenticateUsername)

	s := &http.Server{
		Handler: withCORS(
			service.WithAuthorization(
				middleware.Wrap(handler),
			),
		),
		Addr: fmt.Sprintf("0.0.0.0:%d", port),
		// We remove the timeouts as the connection is terminated by closing of the sandbox and keepalive close.
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  idleTimeout,
	}
	httpserver.ConfigureH2C(s)

	// TODO: Not used anymore in template build, replaced by direct envd command call.
	if startCmdFlag != "" {
		tag := "startCmd"
		cwd := "/home/user"
		user, err := permissions.GetUser("root")
		if err != nil {
			log.Fatalf("error getting user: %v", err) //nolint:gocritic // probably fine to bail if we're done?
		}

		if err = processService.InitializeStartProcess(ctx, user, &processSpec.StartRequest{
			Tag: &tag,
			Process: &processSpec.ProcessConfig{
				Envs: make(map[string]string),
				Cmd:  "/bin/bash",
				Args: []string{"-l", "-c", startCmdFlag},
				Cwd:  &cwd,
			},
		}); err != nil {
			log.Fatalf("error starting process: %v", err)
		}
	}

	// Bind all open ports on 127.0.0.1 and localhost to the eth0 interface
	portScanner := publicport.NewScanner(portScannerInterval)
	defer portScanner.Destroy()

	portLogger := l.With().Str("logger", "port-forwarder").Logger()
	portForwarder := publicport.NewForwarder(&portLogger, portScanner, cgroupManager)
	go portForwarder.StartForwarding(ctx)

	go portScanner.ScanAndBroadcast()

	if err := s.ListenAndServe(); err != nil {
		log.Fatalf("error starting server: %v", err)
	}
}

// readSandboxSecret resolves the HMAC-V2 secret used by runtime.v1 auth.
// Returns nil (no secret) on any failure; main.go warns and skips mount.
func readSandboxSecret(logger *zerolog.Logger) []byte {
	if env := os.Getenv("MAXICORE_SANDBOX_SECRET"); env != "" {
		return []byte(env)
	}
	const secretPath = "/etc/maxicore/sandbox-secret"
	data, err := os.ReadFile(secretPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn().Err(err).Str("path", secretPath).Msg("readSandboxSecret: stat failed")
		}
		return nil
	}
	// Trim trailing newline if present (file-based secrets often have one)
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func createCgroupManager() (m cgroups.Manager) {
	defer func() {
		if m == nil {
			fmt.Fprintf(os.Stderr, "falling back to no-op cgroup manager\n")
			m = cgroups.NewNoopManager()
		}
	}()

	metrics, err := host.GetMetrics()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to calculate host metrics: %v\n", err)

		return nil
	}

	// try to keep 1/8 of the memory free, but no more than 128 MB
	maxMemoryReserved := min(metrics.MemTotal/8, uint64(128)*megabyte)
	memoryMax := metrics.MemTotal - maxMemoryReserved
	memoryHigh := memoryMax // same as memory.max — OOM-kill immediately when throttling can't reclaim enough

	opts := []cgroups.Cgroup2ManagerOption{
		cgroups.WithCgroup2ProcessType(cgroups.ProcessTypePTY, "ptys", map[string]string{
			"cpu.weight":  "200", // gets much preferred cpu access, to help keep these real time
			"memory.high": fmt.Sprintf("%d", memoryHigh),
			"memory.max":  fmt.Sprintf("%d", memoryMax),
		}),
		cgroups.WithCgroup2ProcessType(cgroups.ProcessTypeSocat, "socats", map[string]string{
			"cpu.weight": "150", // gets slightly preferred cpu access
			"memory.min": fmt.Sprintf("%d", 5*megabyte),
			"memory.low": fmt.Sprintf("%d", 8*megabyte),
		}),
		cgroups.WithCgroup2ProcessType(cgroups.ProcessTypeUser, "user", map[string]string{
			"memory.high": fmt.Sprintf("%d", memoryHigh),
			"memory.max":  fmt.Sprintf("%d", memoryMax),
			"cpu.weight":  "50", // less than envd, and less than core processes that default to 100
		}),
	}
	if cgroupRoot != "" {
		opts = append(opts, cgroups.WithCgroup2RootSysFSPath(cgroupRoot))
	}

	mgr, err := cgroups.NewCgroup2Manager(opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create cgroup2 manager: %v\n", err)

		return nil
	}

	return mgr
}
