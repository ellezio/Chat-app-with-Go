package log

import (
	"context"
	"log/slog"
)

var DefaultContextLogger *slog.Logger

type ctxKey struct{}

var logCtxKey ctxKey

func Ctx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(logCtxKey).(*slog.Logger); ok {
		return l
	}

	return DefaultContextLogger
}

func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, logCtxKey, l)
}
