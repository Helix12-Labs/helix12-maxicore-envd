package filesystem

import (
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/execcontext"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/utils"
)

func mockService() Service {
	return Service{
		defaults: &execcontext.Defaults{
			EnvVars: utils.NewMap[string, string](),
		},
	}
}
