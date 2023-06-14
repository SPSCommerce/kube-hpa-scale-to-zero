package internal

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	autoscaling "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/metrics/pkg/client/custom_metrics"
	"k8s.io/metrics/pkg/client/external_metrics"
	"runtime/debug"
	"sync"
	"time"
)

func UnmarshalCurrentMetrics(hpa *autoscaling.HorizontalPodAutoscaler) (*[]autoscaling.MetricStatus, error) {
	hpaMetricsRaw := hpa.ObjectMeta.Annotations["autoscaling.alpha.kubernetes.io/current-metrics"]

	if hpaMetricsRaw == "" {
		return nil, fmt.Errorf("unexpected response from kube: no 'current-metrics' annotation exists")
	}

	var currentMetrics []autoscaling.MetricStatus
	err := json.Unmarshal([]byte(hpaMetricsRaw), &currentMetrics)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal k8s response: %s", err)
	}

	return &currentMetrics, nil
}

func UnmarshallMetricSpecs(hpa *autoscaling.HorizontalPodAutoscaler) (*[]autoscaling.MetricSpec, error) {
	hpaMetricsRaw := hpa.ObjectMeta.Annotations["autoscaling.alpha.kubernetes.io/metrics"]

	if hpaMetricsRaw == "" {
		return nil, fmt.Errorf("no 'metrics' annotation exists")
	}

	var metricSpecs []autoscaling.MetricSpec
	err := json.Unmarshal([]byte(hpaMetricsRaw), &metricSpecs)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal k8s response")
	}

	return &metricSpecs, nil
}

func BuildMetricsSelector(selector *metav1.LabelSelector) (labels.Selector, error) {

	if selector == nil {
		return labels.NewSelector(), nil
	}

	if selector.MatchExpressions != nil {
		return nil, fmt.Errorf("metric selector 'MatchExpressions' is not supported")
	}

	return labels.ValidatedSelectorFromSet(selector.MatchLabels)
}

func RequestIfCustomMetricValueIsZero(customMetric custom_metrics.CustomMetricsClient,
	namespace string, targetName string, spec *autoscaling.MetricSpec) (bool, error) {

	if spec.Type == "Object" {
		selector, err := BuildMetricsSelector(spec.Object.Selector)

		if err != nil {
			return false, fmt.Errorf("unable to create selector: %s", err)
		}

		if spec.Object.Target.Kind == "Deployment" {

			result, err := customMetric.NamespacedMetrics(namespace).GetForObject(schema.GroupKind{
				Group: "apps",
				Kind:  "Deployment",
			}, targetName, spec.Object.MetricName, selector)

			if err != nil {
				return false, err
			}

			return result.Value.IsZero(), nil
		}

		return false, fmt.Errorf("unsupported metric spec target kind %s", spec.Object.Target.Kind)
	}

	return false, fmt.Errorf("unsupported metric spec type '%s'", spec.Type)
}

func RequestIfExternalMetricValueIsZero(client external_metrics.ExternalMetricsClient,
	namespace string,
	spec *autoscaling.MetricSpec) (bool, error) {

	selector, err := BuildMetricsSelector(spec.External.MetricSelector)
	if err != nil {
		return false, fmt.Errorf("unable to create selector: %s", err)
	}

	metrics, err := client.NamespacedMetrics(namespace).List(spec.External.MetricName, selector)

	if err != nil {
		return false, fmt.Errorf("unable list metrics: %s", err)
	}

	if len(metrics.Items) > 1 {
		return false, fmt.Errorf("multiple external metric has been fetched at the same time")
	}

	metric := metrics.Items[0]

	return metric.Value.IsZero(), nil
}

func RequestIfMetricValueIsZero(customMetric custom_metrics.CustomMetricsClient,
	externalMetric external_metrics.ExternalMetricsClient,
	spec *autoscaling.MetricSpec,
	namespace string, targetName string) (bool, error) {
	if spec.Type == "Object" {
		return RequestIfCustomMetricValueIsZero(customMetric, namespace, targetName, spec)
	} else if spec.Type == "External" {
		return RequestIfExternalMetricValueIsZero(externalMetric, namespace, spec)
	}

	return false, fmt.Errorf("unknown metric spec type %s", spec.Type)
}

func IsMetricValueZero(status autoscaling.MetricStatus) (bool, error) {
	if status.Type == "" {
		return false, fmt.Errorf("metic type is empty")
	} else if status.Type == "Object" {
		return status.Object.CurrentValue.IsZero(), nil
	} else if status.Type == "External" {
		return status.External.CurrentValue.IsZero(), nil
	}

	return false, fmt.Errorf("unsupported metric type '%s'", status.Type)
}

func RequestMetricValuesFromSpec(customMetrics custom_metrics.CustomMetricsClient,
	externalMetrics external_metrics.ExternalMetricsClient,
	hpa *autoscaling.HorizontalPodAutoscaler) (*[]MetricValue, error) {

	spec, err := UnmarshallMetricSpecs(hpa)

	if err != nil {
		return nil, fmt.Errorf("unable to unmarchall metric spec for hpa %s.%s: %s",
			hpa.Namespace,
			hpa.Name,
			err)
	}

	var wg sync.WaitGroup
	wg.Add(len(*spec))

	result := make([]MetricValue, len(*spec))
	for i, metric := range *spec {

		go func(metric *autoscaling.MetricSpec, number int, result *[]MetricValue, wg *sync.WaitGroup) {
			defer wg.Done()

			isZero, err := RequestIfMetricValueIsZero(customMetrics, externalMetrics, metric, hpa.Namespace, hpa.Spec.ScaleTargetRef.Name)

			if err != nil {
				(*result)[number] = MetricValue{
					IsZero: false,
					Error:  fmt.Errorf("unable to request metric: %s", err),
				}
			} else {
				(*result)[number] = MetricValue{
					IsZero: isZero,
					Error:  nil,
				}
			}
		}(&metric, i, &result, &wg)
	}
	wg.Wait()

	return &result, nil
}

func ExtractMetricValuesFromCurrentMetrics(hpa *autoscaling.HorizontalPodAutoscaler) (*[]MetricValue, error) {

	currentMetrics, err := UnmarshalCurrentMetrics(hpa)

	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal current metric: %s", err)
	}

	result := make([]MetricValue, len(*currentMetrics))
	for i, metric := range *currentMetrics {
		isZero, err := IsMetricValueZero(metric)

		result[i] = MetricValue{
			IsZero: isZero,
			Error:  err,
		}
	}

	return &result, nil
}

func AnalyzeMetricValues(metricValues *[]MetricValue) (MetricsState, error) {
	for _, response := range *metricValues {
		if response.Error != nil {
			return MetricsState(SomeMetricIsNotZero), response.Error
		}

		if !response.IsZero {
			return MetricsState(SomeMetricIsNotZero), nil
		}
	}

	return MetricsState(AllMetricsAreZero), nil
}

func GetActualMetricsValue(customMetrics custom_metrics.CustomMetricsClient,
	externalMetrics external_metrics.ExternalMetricsClient,
	hpa *autoscaling.HorizontalPodAutoscaler) (*[]MetricValue, error) {

	if hpa.Status.CurrentReplicas == 0 {
		//kube will not return current values if amount of replicas is 0, so we need to check every metric manually
		return RequestMetricValuesFromSpec(customMetrics, externalMetrics, hpa)
	} else {
		return ExtractMetricValuesFromCurrentMetrics(hpa)
	}
}

func ScaleDeployment(ctx context.Context, client *kubernetes.Clientset, namespace string, deploymentName string, desiredReplicasCount int) error {
	payload, err := json.Marshal(DeploymentReplicasPatch{
		Spec: DeploymentReplicasSpec{
			Replicas: desiredReplicasCount,
		},
	})

	if err != nil {
		return fmt.Errorf("unable to generate patch")
	}

	_, err = client.AppsV1().Deployments(namespace).Patch(ctx, deploymentName, types.StrategicMergePatchType,
		payload, metav1.PatchOptions{})

	if err != nil {
		return fmt.Errorf("unable to patch deployment: %s", err)
	}

	return nil
}

func ScaleHpaTarget(ctx context.Context, client *kubernetes.Clientset, targetKind string, namespace string, targetName string, desiredReplicasCount int) error {
	if targetKind == "Deployment" {
		return ScaleDeployment(ctx, client, namespace, targetName, desiredReplicasCount)
	}

	// HPA supports more different kinds as `scaleTargetRef`, so far we can scale only `Deployment`
	return fmt.Errorf("unexpected target kind: %s", targetKind)
}

func ActualizeHpaTargetState(
	ctx context.Context,
	logger *zap.SugaredLogger,
	client *kubernetes.Clientset,
	customMetrics custom_metrics.CustomMetricsClient,
	externalMetrics external_metrics.ExternalMetricsClient,
	prometheus *HpaScopedEventsMetrics,
	hpa *autoscaling.HorizontalPodAutoscaler) *TrackableError {

	metricValues, err := GetActualMetricsValue(customMetrics, externalMetrics, hpa)

	if err != nil {
		return &TrackableError{
			ErrorType:  ErrorTypeMetrics,
			Msg:        "unable to fetch actual metrics",
			InnerError: err,
		}
	}

	actualMetricsState, err := AnalyzeMetricValues(metricValues)

	if err != nil {
		return &TrackableError{
			ErrorType:  ErrorTypeMetrics,
			Msg:        "at least one metric is in invalid state",
			InnerError: err,
		}
	}

	if actualMetricsState == AllMetricsAreZero && hpa.Status.CurrentReplicas != 0 {
		logger.Info("Should be scaled down to 0")

		err := ScaleHpaTarget(ctx, client, hpa.Spec.ScaleTargetRef.Kind, hpa.Namespace, hpa.Spec.ScaleTargetRef.Name, 0)

		if err != nil {
			return &TrackableError{
				ErrorType:  ErrorTypeScaling,
				Msg:        "was not able to scale to 0",
				InnerError: err,
			}
		} else {
			prometheus.ScaleIn.Inc()
		}
	} else if actualMetricsState == SomeMetricIsNotZero && hpa.Status.CurrentReplicas == 0 {
		logger.Info("Should be scaled up to 1")

		err := ScaleHpaTarget(ctx, client, hpa.Spec.ScaleTargetRef.Kind, hpa.Namespace, hpa.Spec.ScaleTargetRef.Name, 1)

		if err != nil {
			return &TrackableError{
				ErrorType:  ErrorTypeScaling,
				Msg:        "was not able to scale to 1",
				InnerError: err,
			}
		} else {
			prometheus.ScaleOut.Inc()
		}
	} else {
		//logger.Debug("is in correct state")
	}

	return nil
}

func MonitorHpa(ctx context.Context,
	initialHpaState *autoscaling.HorizontalPodAutoscaler,
	logger *zap.SugaredLogger,
	client *kubernetes.Clientset,
	customMetrics custom_metrics.CustomMetricsClient,
	externalMetrics external_metrics.ExternalMetricsClient,
	metricsContext MetricsContext,
	channel <-chan *autoscaling.HorizontalPodAutoscaler) {

	defer func() {
		err := recover()
		if err != nil {
			metricsContext.Panics.Inc()
			logger.Errorf("PANIC '%s' occured at %s", err.(error), debug.Stack())
		}
	}()

	metricsContext.RegisterNewHpa(initialHpaState.UID, initialHpaState.Namespace, initialHpaState.Name)

	for hpa := range channel {
		logger := logger.With("uid", hpa.UID,
			"namespace", hpa.Namespace,
			"name", hpa.Name)

		err := ActualizeHpaTargetState(ctx, logger, client, customMetrics, externalMetrics, metricsContext.Events[hpa.UID], hpa)

		if err != nil {
			// it will take some time for k8s to actualize HPA state, so we should not track these errors as real errors
			if hpa.ObjectMeta.CreationTimestamp.Time.Add(3 * time.Minute).After(time.Now()) {
				logger.Infof("Not able to process newly-created HPA: %s", err)
			} else {
				logger.Errorf("not able to process HPA: %s", err)
				metricsContext.TrackError(hpa.UID, err)
			}
		}
	}

	metricsContext.DeregisterHpa(initialHpaState.UID)
}

func SetupHpaInformer(ctx context.Context,
	logger *zap.SugaredLogger,
	client *kubernetes.Clientset,
	customMetrics custom_metrics.CustomMetricsClient,
	externalMetrics external_metrics.ExternalMetricsClient,
	metrics MetricsContext,
	hpaSelector string) {

	factory := informers.NewSharedInformerFactoryWithOptions(client, 60*time.Second,
		informers.WithNamespace(metav1.NamespaceAll),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = hpaSelector
		}))

	hpaInformer := factory.Autoscaling().V1().HorizontalPodAutoscalers().Informer()

	go factory.Start(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), hpaInformer.HasSynced) {
		logger.Fatal("timed out waiting for caches to sync")
	}

	channels := make(map[types.UID]chan *autoscaling.HorizontalPodAutoscaler)

	_, err := hpaInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {

			hpa := obj.(*autoscaling.HorizontalPodAutoscaler)

			channel := make(chan *autoscaling.HorizontalPodAutoscaler)

			channels[hpa.UID] = channel

			logger.Infow("New hpa has been detected ", "uid", hpa.UID)
			go MonitorHpa(ctx, hpa, logger.Named("HpaMonitor"), client, customMetrics, externalMetrics, metrics, channel)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			hpa := newObj.(*autoscaling.HorizontalPodAutoscaler)
			channels[hpa.UID] <- hpa
		},
		DeleteFunc: func(obj interface{}) {
			hpa := obj.(*autoscaling.HorizontalPodAutoscaler)

			close(channels[hpa.UID])
			delete(channels, hpa.UID)

			logger.Infow("Monitoring has been stopped", "uid", hpa.UID)
		},
	})

	if err != nil {
		logger.Fatalf("Unable to subscribe to k8s events: %s", err)
	}

	<-ctx.Done()
}
