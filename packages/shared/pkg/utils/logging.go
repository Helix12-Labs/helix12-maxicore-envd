package utils

import (
	"context"

	"go.uber.org/zap"

	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/shared/pkg/logger"
)

func CleanupCtx(ctx context.Context, msg string, fn func(ctx context.Context) error) {
	if err := fn(ctx); err != nil {
		logger.L().Warn(ctx, msg, zap.Error(err))
	}
}

func Cleanup(ctx context.Context, msg string, fn func() error) {
	if err := fn(); err != nil {
		logger.L().Warn(ctx, msg, zap.Error(err))
	}
}
