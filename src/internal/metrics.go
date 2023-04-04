package internal

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/apimachinery/pkg/types"
)

type HpaScopedErrorMetrics struct {
	Metric  prometheus.Counter
	Scaling prometheus.Counter
}

type HpaScopedEventsMetrics struct {
	ScaleOut prometheus.Counter
	ScaleIn  prometheus.Counter
}

type MetricsContext struct {
	namespace string

	HpaAmount prometheus.Gauge
	Panics    prometheus.Counter
	Events    map[types.UID]*HpaScopedEventsMetrics
	Errors    map[types.UID]*HpaScopedErrorMetrics
}

func RegisterMetricsContext(namespace string) MetricsContext {
	return MetricsContext{
		namespace: namespace,

		HpaAmount: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "total_hpa",
		}),
		Panics: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "scale_to_zero",
			Name:      "panics",
		}),
		Events: make(map[types.UID]*HpaScopedEventsMetrics),
		Errors: make(map[types.UID]*HpaScopedErrorMetrics),
	}
}

func (metrics MetricsContext) RegisterNewHpa(uid types.UID, namespace string, name string) {
	metrics.HpaAmount.Inc()

	metrics.Errors[uid] = &HpaScopedErrorMetrics{
		Metric: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: metrics.namespace,
			Name:      "errors",
			ConstLabels: map[string]string{
				"type":             "metric",
				"target_namespace": namespace,
				"target_hpa":       name,
			},
		}),
		Scaling: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: metrics.namespace,
			Name:      "errors",
			ConstLabels: map[string]string{
				"type":             "scaling",
				"target_namespace": namespace,
				"target_hpa":       name,
			},
		}),
	}
	metrics.Events[uid] = &HpaScopedEventsMetrics{
		ScaleIn: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: metrics.namespace,
			Name:      "events",
			ConstLabels: map[string]string{
				"type":             "scale_in",
				"target_namespace": namespace,
				"target_hpa":       name,
			},
		}),
		ScaleOut: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: metrics.namespace,
			Name:      "events",
			ConstLabels: map[string]string{
				"type":             "scale_out",
				"target_namespace": namespace,
				"target_hpa":       name,
			},
		}),
	}
}

func (metrics MetricsContext) DeregisterHpa(uid types.UID) {
	metrics.HpaAmount.Dec()

	delete(metrics.Errors, uid)
	delete(metrics.Events, uid)
}

func (metrics MetricsContext) TrackError(uid types.UID, err error) {
	if traceable, ok := err.(*TrackableError); ok {
		errorType := traceable.ErrorType

		if errorType == ErrorTypeScaling {
			metrics.Errors[uid].Scaling.Inc()
		} else if errorType == ErrorTypeMetrics {
			metrics.Errors[uid].Metric.Inc()
		} else {
			panic(fmt.Errorf("unknown error type: %d", errorType))
		}
	} else {
		panic("we are trying to track unexpected error type")
	}
}
