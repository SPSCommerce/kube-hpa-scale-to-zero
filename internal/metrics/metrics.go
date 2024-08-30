package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	HpaAmount prometheus.Gauge
	Panics    prometheus.Counter

	eventsMetric *prometheus.CounterVec
	errorsMetric *prometheus.CounterVec
)

func ReportScaleIn(namespace string, hpaName string) {
	eventsMetric.WithLabelValues(namespace, hpaName, "scale_in").Inc()
}
func ReportScaleOut(namespace string, hpaName string) {
	eventsMetric.WithLabelValues(namespace, hpaName, "scale_out").Inc()
}

func ReportNotSupported(namespace string, hpaName string) {
	errorsMetric.WithLabelValues(namespace, hpaName, "not_supported").Inc()
}

func ReportBadHpaState(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "hpa_state").Inc()
}
func ReportCustomMetricError(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "custom_metric").Inc()
}
func ReportExternalMetricError(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "external_metric").Inc()
}
func ReportScalingError(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "scaling_error").Inc()
}

func init() {
	HpaAmount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "panics",
		})
	Panics = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "total_hpa",
		})

	eventsMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "events",
		},
		[]string{"target_namespace", "target_hpa", "type"})

	errorsMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors",
		},
		[]string{"target_namespace", "target_hpa", "type"})

	metrics.Registry.MustRegister(HpaAmount, Panics, errorsMetric, eventsMetric)
}
