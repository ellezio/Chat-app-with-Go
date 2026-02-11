package log

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

var DefaultContextLogger *slog.Logger

type logKey struct{}

var logCtxKey logKey

func Ctx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(logCtxKey).(*slog.Logger); ok {
		return l
	}

	return DefaultContextLogger
}

func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, logCtxKey, l)
}

type correlationIdKey struct{}

var correlationIdCtxKey correlationIdKey

const CorrelationIdHeader = "X-Correlation-Id"

func CorrelationIdCtx(ctx context.Context) string {
	if c, ok := ctx.Value(correlationIdCtxKey).(string); ok {
		return c
	}
	return ""
}

func withCorrelationIdContext(ctx context.Context, correlationId string) context.Context {
	return context.WithValue(ctx, correlationIdCtxKey, correlationId)
}

func Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		correlationId := r.Header.Get(CorrelationIdHeader)
		if correlationId == "" {
			correlationId = uuid.NewString()
		}

		reqLogger := logger.With(
			slog.String("correlation_id", correlationId),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)

		ctx := WithContext(r.Context(), reqLogger)
		ctx = withCorrelationIdContext(ctx, correlationId)

		reqLogger.Info("request started")
		next.ServeHTTP(w, r.WithContext(ctx))
		reqLogger.Info("request completed", slog.Duration("duration", time.Duration(time.Since(start))))
	})
}
