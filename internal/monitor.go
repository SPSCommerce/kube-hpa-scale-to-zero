package internal

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"os"
	"runtime/debug"
	"sync"
	"time"

	metrics "github.com/SPSCommerce/kube-hpa-scale-to-zero/internal/metrics"
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
)

type hpaScopedContext struct {
	context.Context
	hpa                   *autoscaling.HorizontalPodAutoscaler
	logger                *logr.Logger
	kubeClient            *kubernetes.Clientset
	customMetricsClient   custom_metrics.CustomMetricsClient
	externalMetricsClient external_metrics.ExternalMetricsClient
}

func buildMetricsSelector(ctx hpaScopedContext, selector *metav1.LabelSelector) (*labels.Selector, error) {

	if selector == nil {
		sel := labels.NewSelector()
		return &sel, nil
	}

	if selector.MatchExpressions != nil {
		metrics.ReportNotSupported(ctx.hpa.Namespace, ctx.hpa.Name)
		return nil, fmt.Errorf("'matchExpressions' selector is not supported")
	}

	sel, err := labels.ValidatedSelectorFromSet(selector.MatchLabels)
	if err != nil {
		return nil, err
	}

	return &sel, nil
}

func requestIfObjectMetricValueIsZero(ctx hpaScopedContext, spec *autoscaling.MetricSpec) (bool, error) {

	selector, err := buildMetricsSelector(ctx, spec.Object.Selector)

	if err != nil {
		return false, fmt.Errorf("not able to build selector for custom metric: %s", err)
	}

	var group string
	if spec.Object.Target.Kind == "Service" {
		group = ""
	} else if spec.Object.Target.Kind == "Deployment" {
		group = "apps"
	} else {
		metrics.ReportNotSupported(ctx.hpa.Namespace, ctx.hpa.Name)
		return false, fmt.Errorf("unsupported metric target kind %s", spec.Object.Target.Kind)
	}

	result, err := ctx.customMetricsClient.NamespacedMetrics(ctx.hpa.Namespace).GetForObject(schema.GroupKind{
		Group: group,
		Kind:  spec.Object.Target.Kind,
	}, spec.Object.Target.Name, spec.Object.MetricName, *selector)

	if err != nil {
		return false, fmt.Errorf("not able to get metric %s from %s %s: %s", spec.Object.MetricName,
			group,
			spec.Object.Target.Name, err)
	}

	return result.Value.IsZero(), nil

}

func requestIfExternalMetricValueIsZero(ctx hpaScopedContext, spec *autoscaling.MetricSpec) (bool, error) {

	selector, err := buildMetricsSelector(ctx, spec.External.MetricSelector)
	if err != nil {
		return false, fmt.Errorf("not able to build selector for external metric: %s", err)
	}

	metricsList, err := ctx.externalMetricsClient.NamespacedMetrics(ctx.hpa.Namespace).List(spec.External.MetricName, *selector)

	if err != nil {
		metrics.ReportExternalMetricError(ctx.hpa.Namespace, ctx.hpa.Name)
		return false, fmt.Errorf("not able to list external metric %s: %s", spec.External.MetricName, err)
	}

	if len(metricsList.Items) == 0 {
		metrics.ReportExternalMetricError(ctx.hpa.Namespace, ctx.hpa.Name)
		return false, fmt.Errorf("no external metric %s available", spec.External.MetricName)
	}

	for _, metric := range metricsList.Items {
		if !metric.Value.IsZero() {
			return false, nil
		}
	}

	return true, nil
}

func requestMetricValuesFromSpec(ctx hpaScopedContext) (*[]bool, error) {

	hpaMetricsRaw := ctx.hpa.ObjectMeta.Annotations["autoscaling.alpha.kubernetes.io/metrics"]

	if hpaMetricsRaw == "" {
		metrics.ReportBadHpaState(ctx.hpa.Namespace, ctx.hpa.Name)
		return nil, fmt.Errorf("hpa doesn't contain 'metrics' annotation")
	}

	var spec []autoscaling.MetricSpec
	err := json.Unmarshal([]byte(hpaMetricsRaw), &spec)
	if err != nil {
		return nil, fmt.Errorf("not able to unmarshal k8s response")
	}

	var wg sync.WaitGroup
	wg.Add(len(spec))

	metricValues := make(chan bool, len(spec))

	for _, metric := range spec {

		go func(metric *autoscaling.MetricSpec, wg *sync.WaitGroup) {
			defer wg.Done()

			if metric.Type == "Object" {
				isZero, err := requestIfObjectMetricValueIsZero(ctx, metric)

				if err != nil {
					metrics.ReportCustomMetricError(ctx.hpa.Namespace, ctx.hpa.Name)
					ctx.logger.Error(err, "not able to get custom metric")
				} else {
					metricValues <- isZero
				}

			} else if metric.Type == "External" {
				isZero, err := requestIfExternalMetricValueIsZero(ctx, metric)

				if err != nil {
					metrics.ReportExternalMetricError(ctx.hpa.Namespace, ctx.hpa.Name)
					ctx.logger.Error(err, "not able to get external metric")
				} else {
					metricValues <- isZero
				}
			} else if metric.Type == "" {
				metrics.ReportBadHpaState(ctx.hpa.Namespace, ctx.hpa.Name)
				ctx.logger.Error(nil, fmt.Sprintf("unexpected response: hpa returned metric definition with no type"))
			} else {
				metrics.ReportNotSupported(ctx.hpa.Namespace, ctx.hpa.Name)
				ctx.logger.Error(nil, fmt.Sprintf("not supported metric type '%s'", metric.Type))
			}
		}(&metric, &wg)
	}
	wg.Wait()
	close(metricValues)

	if len(metricValues) != len(spec) {
		return nil, fmt.Errorf("not able to get at least one of metrics")
	}

	boolResult := make([]bool, len(spec))
	i := 0
	for result := range metricValues {
		boolResult[i] = result
		i += 1
	}

	return &boolResult, nil
}

func extractMetricValuesFromCurrentMetrics(hpa *autoscaling.HorizontalPodAutoscaler) (*[]bool, error) {
	hpaMetricsRaw := hpa.ObjectMeta.Annotations["autoscaling.alpha.kubernetes.io/current-metrics"]

	if hpaMetricsRaw == "" {
		metrics.ReportBadHpaState(hpa.Namespace, hpa.Name)
		return nil, fmt.Errorf("unexpected response from kube: no 'current-metrics' annotation exists")
	}

	var currentMetrics []autoscaling.MetricStatus
	err := json.Unmarshal([]byte(hpaMetricsRaw), &currentMetrics)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal k8s response: %s", err)
	}

	result := make([]bool, len(currentMetrics))
	for i, metric := range currentMetrics {

		var isZero bool

		if metric.Type == "Object" {
			isZero = metric.Object.CurrentValue.IsZero()
		} else if metric.Type == "External" {
			isZero = metric.External.CurrentValue.IsZero()
		} else if metric.Type == "" {
			metrics.ReportBadHpaState(hpa.Namespace, hpa.Name)
			return nil, fmt.Errorf("unexpected response: hpa returned metric with no type")
		} else {
			metrics.ReportNotSupported(hpa.Namespace, hpa.Name)
			return nil, fmt.Errorf("not supported metric type %q", metric.Type)
		}

		result[i] = isZero
	}

	return &result, nil
}

func checkAllAreZero(metricValues *[]bool) bool {
	for _, value := range *metricValues {
		if !value {
			return false
		}
	}

	return true
}

func scaleDeployment(ctx hpaScopedContext, namespace string, deploymentName string, desiredReplicasCount int) error {
	payload, err := json.Marshal(DeploymentReplicasPatch{
		Spec: DeploymentReplicasSpec{
			Replicas: desiredReplicasCount,
		},
	})

	if err != nil {
		return fmt.Errorf("unable to generate patch: %s", err)
	}

	_, err = ctx.kubeClient.AppsV1().Deployments(namespace).Patch(ctx, deploymentName, types.StrategicMergePatchType,
		payload, metav1.PatchOptions{})

	if err != nil {
		return fmt.Errorf("unable to patch deployment: %s", err)
	}

	return nil
}

func scaleHpaTarget(ctx hpaScopedContext, targetKind string, namespace string, targetName string, desiredReplicasCount int) error {
	if targetKind == "Deployment" {
		return scaleDeployment(ctx, namespace, targetName, desiredReplicasCount)
	} else {
		// HPA supports more different kinds as `scaleTargetRef`, so far we can scale only `Deployment`
		metrics.ReportNotSupported(ctx.hpa.Namespace, ctx.hpa.Name)
		return fmt.Errorf("target kind %q is not supported", targetKind)
	}
}

func actualizeHpaTargetState(ctx hpaScopedContext) error {

	var metricValues *[]bool
	var err error
	if ctx.hpa.Status.CurrentReplicas == 0 {
		//kube will not return current values if amount of replicas is 0, so we need to check every metric manually
		metricValues, err = requestMetricValuesFromSpec(ctx)

		if err != nil {
			return fmt.Errorf("unable to determine if scaling from 0 is required: %s", err)
		}
	} else {
		metricValues, err = extractMetricValuesFromCurrentMetrics(ctx.hpa)

		if err != nil {
			return fmt.Errorf("unable to determine if scaling to 0 is required: %s", err)
		}
	}

	allAreZero := checkAllAreZero(metricValues)

	if allAreZero && ctx.hpa.Status.CurrentReplicas != 0 {
		ctx.logger.Info("Should be scaled down to 0")

		err := scaleHpaTarget(ctx, ctx.hpa.Spec.ScaleTargetRef.Kind, ctx.hpa.Namespace, ctx.hpa.Spec.ScaleTargetRef.Name, 0)

		if err != nil {
			metrics.ReportScalingError(ctx.hpa.Namespace, ctx.hpa.Name)
			return fmt.Errorf("should have been scaled in to 0")
		} else {
			metrics.ReportScaleIn(ctx.hpa.Namespace, ctx.hpa.Name)
		}
	} else if !allAreZero && ctx.hpa.Status.CurrentReplicas == 0 {
		ctx.logger.Info("Should be scaled up to 1")

		err := scaleHpaTarget(ctx, ctx.hpa.Spec.ScaleTargetRef.Kind, ctx.hpa.Namespace, ctx.hpa.Spec.ScaleTargetRef.Name, 1)

		if err != nil {
			metrics.ReportScalingError(ctx.hpa.Namespace, ctx.hpa.Name)
			return fmt.Errorf("should have been scaled out to 1")
		} else {
			metrics.ReportScaleOut(ctx.hpa.Namespace, ctx.hpa.Name)
		}
	} else {
		//logger.Debug("nothing to do")
	}

	return nil
}

func actualizeHpaState(ctx context.Context,
	logger *logr.Logger,
	kubeClient *kubernetes.Clientset,
	customMetricsClient custom_metrics.CustomMetricsClient,
	externalMetricsClient external_metrics.ExternalMetricsClient,
	channel <-chan *autoscaling.HorizontalPodAutoscaler) {

	defer func() {
		err := recover()
		if err != nil {
			metrics.Panics.Inc()
			logger.Error(err.(error), "PANIC occured at %s", debug.Stack())
		}
	}()

	for hpa := range channel {

		hpaLogger := logger.WithValues("uid", hpa.UID, "namespace", hpa.Namespace, "name", hpa.Name)

		ctx := hpaScopedContext{
			Context:               ctx,
			hpa:                   hpa,
			logger:                &hpaLogger,
			kubeClient:            kubeClient,
			customMetricsClient:   customMetricsClient,
			externalMetricsClient: externalMetricsClient,
		}

		err := actualizeHpaTargetState(ctx)

		if err != nil {
			// it will take some time for k8s to actualize HPA state, so we should not track these errors as real errors
			if hpa.ObjectMeta.CreationTimestamp.Time.Add(3 * time.Minute).After(time.Now()) {
				ctx.logger.Info(fmt.Sprintf("Not able to process newly-created HPA: %s", err))
			} else {
				ctx.logger.Error(err, "not able to process HPA")
			}
		}
	}
}

func SetupHpaInformer(ctx context.Context,
	logger *logr.Logger,
	kubeClient *kubernetes.Clientset,
	customMetricsClient custom_metrics.CustomMetricsClient,
	externalMetricsClient external_metrics.ExternalMetricsClient,
	hpaSelector string) {

	factory := informers.NewSharedInformerFactoryWithOptions(kubeClient, time.Minute,
		informers.WithNamespace(metav1.NamespaceAll),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = hpaSelector
		}))

	hpaInformer := factory.Autoscaling().V1().HorizontalPodAutoscalers().Informer()

	hpaQueue := make(chan *autoscaling.HorizontalPodAutoscaler)
	go actualizeHpaState(ctx, logger, kubeClient, customMetricsClient, externalMetricsClient, hpaQueue)

	go factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), hpaInformer.HasSynced) {
		logger.Error(nil, "timed out waiting for caches to sync")
		os.Exit(1)
	}

	_, err := hpaInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			hpa := obj.(*autoscaling.HorizontalPodAutoscaler)
			logger.Info("New hpa has been detected", "uid", hpa.UID)
			metrics.HpaAmount.Inc()
			hpaQueue <- hpa
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			hpa := newObj.(*autoscaling.HorizontalPodAutoscaler)
			hpaQueue <- hpa
		},
		DeleteFunc: func(obj interface{}) {
			hpa := obj.(*autoscaling.HorizontalPodAutoscaler)
			logger.Info("Monitoring has been stopped", "uid", hpa.UID)
			metrics.HpaAmount.Desc()
		},
	})

	if err != nil {
		logger.Error(err, "Unable to subscribe to k8s events")
		os.Exit(1)
	}

	<-ctx.Done()
}
