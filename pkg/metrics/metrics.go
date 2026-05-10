package metrics

import (
	"context"
	"net/http"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

type Metrics struct {
	artifactsRegistered        metric.Int64Counter
	resolveRequests            metric.Int64Counter
	fallback                   metric.Int64Counter
	grpcRegisterArtifact       metric.Int64Counter
	grpcRegisterArtifactErrors metric.Int64Counter
	grpcResolveHandoff         metric.Int64Counter
	grpcResolveHandoffErrors   metric.Int64Counter
	grpcNotifyNodeTerminal     metric.Int64Counter
	grpcFinalizeSampleRun      metric.Int64Counter
	grpcEvaluateGC             metric.Int64Counter
	grpcGetLifecycle           metric.Int64Counter
	gcBacklogBytes             metric.Float64Gauge
	handler                    http.Handler
}

func New() (*Metrics, error) {
	reg := promclient.NewRegistry()
	exporter, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	meter := provider.Meter("github.com/HeaInSeo/artifact-handoff")

	m := &Metrics{
		handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	}

	counters := []struct {
		dest *metric.Int64Counter
		name string
	}{
		{&m.artifactsRegistered, "ah_artifacts_registered"},
		{&m.resolveRequests, "ah_resolve_requests"},
		{&m.fallback, "ah_fallback"},
		{&m.grpcRegisterArtifact, "ah_grpc_register_artifact"},
		{&m.grpcRegisterArtifactErrors, "ah_grpc_register_artifact_errors"},
		{&m.grpcResolveHandoff, "ah_grpc_resolve_handoff"},
		{&m.grpcResolveHandoffErrors, "ah_grpc_resolve_handoff_errors"},
		{&m.grpcNotifyNodeTerminal, "ah_grpc_notify_node_terminal"},
		{&m.grpcFinalizeSampleRun, "ah_grpc_finalize_sample_run"},
		{&m.grpcEvaluateGC, "ah_grpc_evaluate_gc"},
		{&m.grpcGetLifecycle, "ah_grpc_get_lifecycle"},
	}
	for _, c := range counters {
		*c.dest, err = meter.Int64Counter(c.name)
		if err != nil {
			return nil, err
		}
	}
	if m.gcBacklogBytes, err = meter.Float64Gauge("ah_gc_backlog_bytes"); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) IncArtifactsRegistered()        { m.artifactsRegistered.Add(context.Background(), 1) }
func (m *Metrics) IncResolveRequests()             { m.resolveRequests.Add(context.Background(), 1) }
func (m *Metrics) IncFallback()                    { m.fallback.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCRegisterArtifact()        { m.grpcRegisterArtifact.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCRegisterArtifactErrors()  { m.grpcRegisterArtifactErrors.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCResolveHandoff()          { m.grpcResolveHandoff.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCResolveHandoffErrors()    { m.grpcResolveHandoffErrors.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCNotifyNodeTerminal()      { m.grpcNotifyNodeTerminal.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCFinalizeSampleRun()       { m.grpcFinalizeSampleRun.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCEvaluateGC()              { m.grpcEvaluateGC.Add(context.Background(), 1) }
func (m *Metrics) IncGRPCGetLifecycle()            { m.grpcGetLifecycle.Add(context.Background(), 1) }
func (m *Metrics) SetGCBacklogBytes(v float64)     { m.gcBacklogBytes.Record(context.Background(), v) }
func (m *Metrics) Handler() http.Handler           { return m.handler }
