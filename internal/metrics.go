package internal

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/apimachinery/pkg/types"
)

type HpaScopedErrorMetrics struct {
	NotSupportedError   prometheus.Counter
	HpaStateError       prometheus.Counter
	CustomMetricError   prometheus.Counter
	ExternalMetricError prometheus.Counter
	ScalingError        prometheus.Counter
}

type HpaScopedEventMetrics struct {
	ScaleOutEvent prometheus.Counter
	ScaleInEvent  prometheus.Counter
}

type HpaScopedMetrics struct {
	Errors *HpaScopedErrorMetrics
	Events *HpaScopedEventMetrics
}

type OverallMetrics struct {
	HpaAmount prometheus.Gauge
	Panics    prometheus.Counter
}

type MetricsContext struct {
	namespace string

	Overall *OverallMetrics
	Scoped  map[types.UID]*HpaScopedMetrics
}

func RegisterMetricsContext(namespace string) MetricsContext {
	return MetricsContext{
		namespace: namespace,
		Overall: &OverallMetrics{
			HpaAmount: promauto.NewGauge(prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "total_hpa",
			}),
			Panics: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: "scale_to_zero",
				Name:      "panics",
			}),
		},
		Scoped: make(map[types.UID]*HpaScopedMetrics),
	}
}

func (metrics MetricsContext) RegisterNewHpa(uid types.UID, namespace string, name string) {
	metrics.Overall.HpaAmount.Inc()

	metrics.Scoped[uid] = &HpaScopedMetrics{
		Errors: &HpaScopedErrorMetrics{
			NotSupportedError: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "errors",
				ConstLabels: map[string]string{
					"type":             "not_supported",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
			HpaStateError: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "errors",
				ConstLabels: map[string]string{
					"type":             "hpa_state",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
			CustomMetricError: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "errors",
				ConstLabels: map[string]string{
					"type":             "custom_metric",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
			ExternalMetricError: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "errors",
				ConstLabels: map[string]string{
					"type":             "external_metric",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
			ScalingError: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "errors",
				ConstLabels: map[string]string{
					"type":             "scaling",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
		},
		Events: &HpaScopedEventMetrics{
			ScaleInEvent: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "events",
				ConstLabels: map[string]string{
					"type":             "scale_in",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
			ScaleOutEvent: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: metrics.namespace,
				Name:      "events",
				ConstLabels: map[string]string{
					"type":             "scale_out",
					"target_namespace": namespace,
					"target_hpa":       name,
				},
			}),
		},
	}
}

func (metrics MetricsContext) DeregisterHpa(uid types.UID) {
	metrics.Overall.HpaAmount.Dec()

	delete(metrics.Scoped, uid)
}
