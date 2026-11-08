// Package telemetry provides utilities for configuring OpenTelemetry (otel)
// observability and W3C TraceContext propagation within the Domour agent runtime.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var (
	LogLevel     = new(slog.LevelVar)
	propagatorMu sync.Once
)

func init() {
	EnsurePropagator()
}

// EnsurePropagator guarantees the global W3C TraceContext + Baggage propagator is registered.
func EnsurePropagator() {
	propagatorMu.Do(func() {
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	})
}

func SetLogLevel(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		LogLevel.Set(slog.LevelDebug)
	case "info":
		LogLevel.Set(slog.LevelInfo)
	case "warn", "warning":
		LogLevel.Set(slog.LevelWarn)
	case "error":
		LogLevel.Set(slog.LevelError)
	}
}

type traceContextHandler struct {
	serviceName string
	handlers    []slog.Handler
	level       slog.Leveler
}

func (h *traceContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.level != nil && level < h.level.Level() {
		return false
	}
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *traceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	clone := r.Clone()
	hasService := false
	hasTraceID := false
	hasSpanID := false
	clone.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "service":
			hasService = true
		case "trace_id":
			hasTraceID = true
		case "span_id":
			hasSpanID = true
		}
		return true
	})

	var extra []slog.Attr
	if !hasService && h.serviceName != "" {
		extra = append(extra, slog.String("service", h.serviceName))
	}
	if ctx != nil {
		spanCtx := trace.SpanContextFromContext(ctx)
		if spanCtx.IsValid() {
			if !hasTraceID {
				extra = append(extra, slog.String("trace_id", spanCtx.TraceID().String()))
			}
			if !hasSpanID {
				extra = append(extra, slog.String("span_id", spanCtx.SpanID().String()))
			}
		}
	}
	if len(extra) > 0 {
		clone.AddAttrs(extra...)
	}

	var firstErr error
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, clone.Level) {
			if err := sub.Handle(ctx, clone); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (h *traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		next[i] = sub.WithAttrs(attrs)
	}
	return &traceContextHandler{
		serviceName: h.serviceName,
		handlers:    next,
		level:       h.level,
	}
}

func (h *traceContextHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		next[i] = sub.WithGroup(name)
	}
	return &traceContextHandler{
		serviceName: h.serviceName,
		handlers:    next,
		level:       h.level,
	}
}

type Config struct {
	ServiceName    string
	ServiceVersion string
	Endpoint       string  // OTLP gRPC endpoint (e.g., "localhost:4317")
	Sampler        float64 // Sampling rate (0.0 to 1.0)
	UseStdout      bool    // If true, traces will be exported to stdout instead of OTLP
	ForceReinit    bool    // If true, re-initializes even if host already configured OTel
	ExportAll      bool    // If true, exports OTLP metrics & logs even to local :4317
}

func isLocalTraceOnlyEndpoint(endpoint string) bool {
	if os.Getenv("DOMOUR_OTEL_EXPORT_ALL") == "true" || os.Getenv("COSMOS_OTEL_EXPORT_ALL") == "true" {
		return false
	}
	ep := strings.TrimSpace(strings.ToLower(endpoint))
	return ep == "localhost:4317" || ep == "127.0.0.1:4317" || ep == "0.0.0.0:4317"
}

// IsHostOTelInitialized checks whether an outer host (e.g., cosmos-star or cosmos-assistant)
// has already registered a real SDK TracerProvider on the global otel singleton.
func IsHostOTelInitialized() bool {
	_, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	return ok
}

// Setup initializes the OpenTelemetry SDK with tracing, metrics, and logs.
// When embedded inside a host that has already initialized global OTel, Setup
// respects the Host-First Singleton rule and returns a no-op shutdown function
// unless ForceReinit or UseStdout is explicitly set.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	EnsurePropagator()

	if !cfg.ForceReinit && !cfg.UseStdout && IsHostOTelInitialized() {
		return func(context.Context) error { return nil }, nil
	}

	if cfg.Endpoint == "" && !cfg.UseStdout {
		return func(context.Context) error { return nil }, nil
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "domour"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "dev"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return func(context.Context) error { return nil }, fmt.Errorf("failed to create resource: %w", err)
	}

	// 1. Tracing Setup (100% non-blocking)
	var traceExporter sdktrace.SpanExporter
	if cfg.UseStdout {
		traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	} else if cfg.Endpoint != "" {
		traceExporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
			otlptracegrpc.WithInsecure(),
		)
	}
	if err != nil {
		return func(context.Context) error { return nil }, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.Sampler > 0 && cfg.Sampler < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(cfg.Sampler)
	} else if cfg.Sampler < 0 {
		sampler = sdktrace.NeverSample()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	exportFull := cfg.UseStdout || cfg.ExportAll || !isLocalTraceOnlyEndpoint(cfg.Endpoint)

	// 2. Metrics Setup (Skipped on trace-only local :4317 unless ExportAll/UseStdout)
	var mp *sdkmetric.MeterProvider
	if exportFull {
		var metricExporter sdkmetric.Exporter
		if cfg.UseStdout {
			metricExporter, err = stdoutmetric.New()
		} else if cfg.Endpoint != "" {
			metricExporter, err = otlpmetricgrpc.New(ctx,
				otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
				otlpmetricgrpc.WithInsecure(),
			)
		}
		if err == nil && metricExporter != nil {
			mp = sdkmetric.NewMeterProvider(
				sdkmetric.WithResource(res),
				sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
			)
			otel.SetMeterProvider(mp)
		}
	}

	// 3. Logs Setup (Dual-Track: preserve console output + optional OTLP log bridge)
	var lp *sdklog.LoggerProvider
	if exportFull {
		var logExporter sdklog.Exporter
		if cfg.UseStdout {
			logExporter, err = stdoutlog.New()
		} else if cfg.Endpoint != "" {
			logExporter, err = otlploggrpc.New(ctx,
				otlploggrpc.WithEndpoint(cfg.Endpoint),
				otlploggrpc.WithInsecure(),
			)
		}
		if err == nil && logExporter != nil {
			lp = sdklog.NewLoggerProvider(
				sdklog.WithResource(res),
				sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
			)
			global.SetLoggerProvider(lp)
		}
	}

	if !cfg.UseStdout {
		consoleHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: LogLevel})
		handlers := []slog.Handler{consoleHandler}
		if lp != nil {
			handlers = append(handlers, otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp)))
		}
		slog.SetDefault(slog.New(&traceContextHandler{
			serviceName: cfg.ServiceName,
			handlers:    handlers,
			level:       LogLevel,
		}))
	}

	shutdown := func(shutdownCtx context.Context) error {
		tCtx, cancel := context.WithTimeout(shutdownCtx, 2*time.Second)
		defer cancel()

		var errs []error
		if err := tp.Shutdown(tCtx); err != nil {
			errs = append(errs, fmt.Errorf("trace shutdown failed: %w", err))
		}
		if mp != nil {
			if err := mp.Shutdown(tCtx); err != nil {
				errs = append(errs, fmt.Errorf("metric shutdown failed: %w", err))
			}
		}
		if lp != nil {
			if err := lp.Shutdown(tCtx); err != nil {
				errs = append(errs, fmt.Errorf("log shutdown failed: %w", err))
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("telemetry shutdown errors: %v", errs)
		}
		return nil
	}

	return shutdown, nil
}

type metadataCarrier metadata.MD

func (mc metadataCarrier) Get(key string) string {
	vals := metadata.MD(mc).Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (mc metadataCarrier) Set(key, val string) {
	metadata.MD(mc).Set(key, val)
}

func (mc metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(mc))
	for k := range mc {
		keys = append(keys, k)
	}
	return keys
}

// ExtractGRPCContext extracts W3C TraceContext (traceparent/tracestate) and Baggage
// from incoming gRPC metadata into ctx.
func ExtractGRPCContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	EnsurePropagator()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, metadataCarrier(md))
}

// InjectGRPCMetadata injects W3C TraceContext from ctx into outgoing gRPC metadata.
func InjectGRPCMetadata(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	EnsurePropagator()
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	} else {
		md = md.Copy()
	}
	otel.GetTextMapPropagator().Inject(ctx, metadataCarrier(md))
	return metadata.NewOutgoingContext(ctx, md)
}

// TraceIDFromContext returns the 32-hex-char W3C trace_id from the active SpanContext,
// or "00000000000000000000000000000000" if none is active.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return "00000000000000000000000000000000"
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		return spanCtx.TraceID().String()
	}
	extracted := ExtractGRPCContext(ctx)
	spanCtx = trace.SpanContextFromContext(extracted)
	if spanCtx.IsValid() {
		return spanCtx.TraceID().String()
	}
	return "00000000000000000000000000000000"
}

// UnaryServerInterceptor extracts W3C TraceContext and starts a server span for unary RPCs.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		ctx = ExtractGRPCContext(ctx)
		ctx, span := otel.Tracer("domour/grpc").Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		return handler(ctx, req)
	}
}

type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedServerStream) Context() context.Context {
	return s.ctx
}

// StreamServerInterceptor extracts W3C TraceContext and starts a server span for streaming RPCs.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx := ExtractGRPCContext(ss.Context())
		ctx, span := otel.Tracer("domour/grpc").Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		return handler(srv, &tracedServerStream{ServerStream: ss, ctx: ctx})
	}
}
