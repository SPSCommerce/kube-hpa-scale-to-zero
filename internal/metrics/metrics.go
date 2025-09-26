package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

func ReportBadHpaState(namespace string, hpaName string) {
	errorsMetric.WithLabelValues(namespace, hpaName, "hpa_state").Inc()
}
func ReportCustomMetricError(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "object_metric").Inc()
}
func ReportExternalMetricError(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "external_metric").Inc()
}
func ReportScalingError(namespace string, volumeType string) {
	errorsMetric.WithLabelValues(namespace, volumeType, "scaling").Inc()
}

func init() {
	HpaAmount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "scale_to_zero",
			Name:      "total_hpa",
		})
	Panics = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "scale_to_zero",
			Name:      "panics",
		})

	eventsMetric = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scale_to_zero",
			Name:      "events",
		},
		[]string{"target_namespace", "target_hpa", "type"})

	errorsMetric = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scale_to_zero",
			Name:      "errors",
		},
		[]string{"target_namespace", "target_hpa", "type"})
}
