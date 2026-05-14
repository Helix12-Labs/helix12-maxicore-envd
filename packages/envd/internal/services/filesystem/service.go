package filesystem

import (
	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/execcontext"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/logs"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/legacy"
	spec "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/spec/filesystem/filesystemconnect"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/utils"
)

type Service struct {
	logger   *zerolog.Logger
	watchers *utils.Map[string, *FileWatcher]
	defaults *execcontext.Defaults
}

func Handle(server *chi.Mux, l *zerolog.Logger, defaults *execcontext.Defaults) {
	service := Service{
		logger:   l,
		watchers: utils.NewMap[string, *FileWatcher](),
		defaults: defaults,
	}

	interceptors := connect.WithInterceptors(
		logs.NewUnaryLogInterceptor(l),
		legacy.Convert(),
	)

	path, handler := spec.NewFilesystemHandler(service, interceptors)

	server.Mount(path, handler)
}
