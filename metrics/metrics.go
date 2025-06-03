// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const (
	MetricsNamespace       = "copilot"
	MetricsSubsystemSystem = "system"
	MetricsSubsystemHTTP   = "http"
	MetricsSubsystemAPI    = "api"
	MetricsSubsystemLLM    = "llm"

	MetricsCloudInstallationLabel = "installationId"
	MetricsVersionLabel           = "version"
)

type Metrics interface {
	GetRegistry() *prometheus.Registry

	ObserveAPIEndpointDuration(handler, method, statusCode string, elapsed float64)

	IncrementHTTPRequests()
	IncrementHTTPErrors()

	GetMetricsForAIService(llmName string) *llmMetrics
}

type InstanceInfo struct {
	InstallationID      string
	ConnectedUsersLimit int
	PluginVersion       string
}

// metrics used to instrumentate metrics in prometheus.
type metrics struct {
	registry *prometheus.Registry

	pluginStartTime prometheus.Gauge
	pluginInfo      prometheus.Gauge

	apiTime *prometheus.HistogramVec

	httpRequestsTotal prometheus.Counter
	httpErrorsTotal   prometheus.Counter

	llmRequestsTotal *prometheus.CounterVec
}

// NewMetrics Factory method to create a new metrics collector.
func NewMetrics(info InstanceInfo) Metrics {
	m := &metrics{}

	m.registry = prometheus.NewRegistry()
	options := collectors.ProcessCollectorOpts{
		Namespace: MetricsNamespace,
	}
	m.registry.MustRegister(collectors.NewProcessCollector(options))
	m.registry.MustRegister(collectors.NewGoCollector())

	additionalLabels := map[string]string{}
	if info.InstallationID != "" {
		additionalLabels[MetricsCloudInstallationLabel] = info.InstallationID
	}

	m.pluginStartTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   MetricsNamespace,
		Subsystem:   MetricsSubsystemSystem,
		Name:        "plugin_start_timestamp_seconds",
		Help:        "The time the plugin started.",
		ConstLabels: additionalLabels,
	})
	m.pluginStartTime.SetToCurrentTime()
	m.registry.MustRegister(m.pluginStartTime)

	m.pluginInfo = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: MetricsNamespace,
		Subsystem: MetricsSubsystemSystem,
		Name:      "plugin_info",
		Help:      "The plugin version.",
		ConstLabels: map[string]string{
			MetricsCloudInstallationLabel: info.InstallationID,
			MetricsVersionLabel:           info.PluginVersion,
		},
	})
	m.pluginInfo.Set(1)
	m.registry.MustRegister(m.pluginInfo)

	m.apiTime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace:   MetricsNamespace,
			Subsystem:   MetricsSubsystemAPI,
			Name:        "time_seconds",
			Help:        "Time to execute the api handler",
			ConstLabels: additionalLabels,
		},
		[]string{"handler", "method", "status_code"},
	)
	m.registry.MustRegister(m.apiTime)

	m.httpRequestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace:   MetricsNamespace,
		Subsystem:   MetricsSubsystemHTTP,
		Name:        "requests_total",
		Help:        "The total number of http API requests.",
		ConstLabels: additionalLabels,
	})
	m.registry.MustRegister(m.httpRequestsTotal)

	m.httpErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace:   MetricsNamespace,
		Subsystem:   MetricsSubsystemHTTP,
		Name:        "errors_total",
		Help:        "The total number of http API errors.",
		ConstLabels: additionalLabels,
	})
	m.registry.MustRegister(m.httpErrorsTotal)

	m.llmRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   MetricsNamespace,
		Subsystem:   MetricsSubsystemLLM,
		Name:        "requests_total",
		Help:        "The total number of LLM requests.",
		ConstLabels: additionalLabels,
	}, []string{"llm_name"})
	m.registry.MustRegister(m.llmRequestsTotal)

	return m
}

func (m *metrics) GetRegistry() *prometheus.Registry {
	return m.registry
}

func (m *metrics) ObserveAPIEndpointDuration(handler, method, statusCode string, elapsed float64) {
	if m != nil {
		m.apiTime.With(prometheus.Labels{"handler": handler, "method": method, "status_code": statusCode}).Observe(elapsed)
	}
}

func (m *metrics) IncrementHTTPRequests() {
	if m != nil {
		m.httpRequestsTotal.Inc()
	}
}

func (m *metrics) IncrementHTTPErrors() {
	if m != nil {
		m.httpErrorsTotal.Inc()
	}
}

func (m *metrics) GetMetricsForAIService(llmName string) *llmMetrics {
	if m == nil {
		return nil
	}

	return &llmMetrics{
		llmRequestsTotal: m.llmRequestsTotal.MustCurryWith(prometheus.Labels{"llm_name": llmName}),
	}
}

type LLMetrics interface {
	IncrementLLMRequests()
}

type llmMetrics struct {
	llmRequestsTotal *prometheus.CounterVec
}

func (m *llmMetrics) IncrementLLMRequests() {
	if m != nil {
		m.llmRequestsTotal.With(prometheus.Labels{}).Inc()
	}
}
