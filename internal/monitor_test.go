package internal

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	v1beta1 "k8s.io/metrics/pkg/apis/external_metrics/v1beta1"
	fakecustommetrics "k8s.io/metrics/pkg/client/custom_metrics/fake"
	fakeexternalmetrics "k8s.io/metrics/pkg/client/external_metrics/fake"
)

func CreateTestHPA(minReplicas *int32, maxReplicas int32, quantity int64, behaviour *autoscalingv2.HorizontalPodAutoscalerBehavior, conditions []autoscalingv2.HorizontalPodAutoscalerCondition) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling/v2",
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
			Labels: map[string]string{
				"scaletozero.spscommerce.com/watch": "true",
			},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "test-deployment",
			},
			MinReplicas: minReplicas,
			MaxReplicas: maxReplicas,
			Behavior:    behaviour,
			Metrics: []autoscalingv2.MetricSpec{

				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{
							Name: "scale_to_zero_demo_replica_count",
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"demo": "demo",
								},
							},
						},
						Target: autoscalingv2.MetricTarget{
							Type:  autoscalingv2.ValueMetricType,
							Value: resource.NewQuantity(quantity, resource.DecimalSI),
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			Conditions:      conditions,
			CurrentReplicas: 1,
			DesiredReplicas: 1,
			LastScaleTime:   &metav1.Time{},
			CurrentMetrics: []autoscalingv2.MetricStatus{
				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricStatus{
						Metric: autoscalingv2.MetricIdentifier{
							Name: "scale_to_zero_demo_replica_count",
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"demo": "demo",
								},
							},
						},
						Current: autoscalingv2.MetricValueStatus{
							Value: resource.NewQuantity(quantity, resource.DecimalSI),
						},
					},
				},
			},
		},
	}
}

func CreateTestDeploy(replicas *int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Ports: []v1.ContainerPort{
								{
									ContainerPort: 80,
									Protocol:      v1.ProtocolTCP,
								},
							},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("100m"),
									v1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("500m"),
									v1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          *replicas,
			ReadyReplicas:     *replicas,
			AvailableReplicas: *replicas,
			UpdatedReplicas:   *replicas,
		},
	}
}

func TestNormalScalingDown(t *testing.T) {

	// Create HPA with current replicas = 1, metric value = 0
	replicas := int32(1)
	minReplicas := int32(1)
	maxReplicas := int32(10)
	quantity := int64(0) // Metric value is 0
	conditions := []autoscalingv2.HorizontalPodAutoscalerCondition{
		{
			Type:               autoscalingv2.AbleToScale,
			Status:             "False",
			LastTransitionTime: metav1.Now(),
			Reason:             "ReadyForNewScale",
			Message:            "recommended size matches current size",
		}}

	hpa := CreateTestHPA(&minReplicas, maxReplicas, quantity, nil, conditions)
	hpa.Status.CurrentReplicas = 1

	deploy := CreateTestDeploy(&replicas)

	fakeKubeClient := fake.NewClientset(deploy)
	fakeExternalMetrics := &fakeexternalmetrics.FakeExternalMetricsClient{}
	fakeCustomMetrics := &fakecustommetrics.FakeCustomMetricsClient{}
	logger := logr.Discard()

	ctx := hpaScopedContext{
		Context:               context.TODO(),
		hpa:                   hpa,
		logger:                &logger,
		kubeClient:            fakeKubeClient,
		customMetricsClient:   fakeCustomMetrics,
		externalMetricsClient: fakeExternalMetrics,
	}

	// Execute the function
	err := actualizeHpaTargetState(ctx)

	// Verify results
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}

	// Check that a patch action was called to scale to 0
	actions := fakeKubeClient.Actions()
	found := false
	for _, action := range actions {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "deployments" {
			// Cast the action to PatchAction to get patch details
			if patchAction, ok := action.(k8stesting.PatchAction); ok {
				t.Logf("Patch action found with data: %s", string(patchAction.GetPatch()))
			}
			found = true

			// Get the deployment to verify it was scaled to 0
			deployment, err := fakeKubeClient.AppsV1().Deployments("default").Get(context.TODO(), "test-deployment", metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get deployment: %v", err)
				t.Fail()
			} else if *deployment.Spec.Replicas != 0 {
				t.Errorf("Expected deployment to be scaled to 0, but got %d replicas", *deployment.Spec.Replicas)
				t.Fail()
			}
			break
		}
	}
	if !found {
		t.Error("Expected deployment patch action to scale to 0, but none was found")
		t.Fail()
	}
}

func TestNormalScalingUp(t *testing.T) {

	replicas := int32(0)
	minReplicas := int32(1)
	maxReplicas := int32(10)
	quantity := int64(1)
	conditions := []autoscalingv2.HorizontalPodAutoscalerCondition{
		{
			Type:               autoscalingv2.AbleToScale,
			Status:             "False",
			LastTransitionTime: metav1.Now(),
			Reason:             "ReadyForNewScale",
			Message:            "recommended size matches current size",
		}}

	hpa := CreateTestHPA(&minReplicas, maxReplicas, quantity, nil, conditions)
	hpa.Status.CurrentReplicas = 0

	deploy := CreateTestDeploy(&replicas)

	fakeKubeClient := fake.NewClientset(deploy)
	fakeExternalMetrics := &fakeexternalmetrics.FakeExternalMetricsClient{}
	fakeCustomMetrics := &fakecustommetrics.FakeCustomMetricsClient{}

	// Add external metric using AddReactor - this mocks the external metrics API
	fakeExternalMetrics.Fake.AddReactor("list", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Return a metric list with the desired metric value
		return true, &v1beta1.ExternalMetricValueList{
			Items: []v1beta1.ExternalMetricValue{
				{
					MetricName: "scale_to_zero_demo_replica_count",
					Value:      *resource.NewQuantity(1, resource.DecimalSI), // This is the metric value that will be returned
					Timestamp:  metav1.Now(),
				},
			},
		}, nil
	})

	logger := logr.Discard()

	ctx := hpaScopedContext{
		Context:               context.TODO(),
		hpa:                   hpa,
		logger:                &logger,
		kubeClient:            fakeKubeClient,
		customMetricsClient:   fakeCustomMetrics,
		externalMetricsClient: fakeExternalMetrics,
	}

	// Execute the function
	err := actualizeHpaTargetState(ctx)

	// Verify results
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}

	// Check that a patch action was called to scale to 0
	actions := fakeKubeClient.Actions()
	found := false
	for _, action := range actions {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "deployments" {
			// Cast the action to PatchAction to get patch details
			if patchAction, ok := action.(k8stesting.PatchAction); ok {
				t.Logf("Patch action found with data: %s", string(patchAction.GetPatch()))
			}
			found = true

			// Get the deployment to verify it was scaled to 1
			deployment, err := fakeKubeClient.AppsV1().Deployments("default").Get(context.TODO(), "test-deployment", metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get deployment: %v", err)
				t.Fail()
			} else if *deployment.Spec.Replicas != 1 {
				t.Errorf("Expected deployment to be scaled to 1, but got %d replicas", *deployment.Spec.Replicas)
				t.Fail()
			}
			break
		}
	}
	if !found {
		t.Error("Expected deployment patch action to scale to 0, but none was found")
		t.Fail()
	}
}

func TestScalingUpWithBehaviourNotAllowed(t *testing.T) {

	replicas := int32(0)
	minReplicas := int32(1)
	maxReplicas := int32(10)
	quantity := int64(1)
	StabilizationWindowSeconds := int32(60)
	conditions := []autoscalingv2.HorizontalPodAutoscalerCondition{
		{
			Type:               autoscalingv2.ScalingActive,
			Status:             "False",
			LastTransitionTime: metav1.Now(),
			Reason:             "ScalingDisabled",
			Message:            "scaling target replicas = 0",
		}}
	behaviour := &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &StabilizationWindowSeconds,
			Policies: []autoscalingv2.HPAScalingPolicy{
				{
					Type:          autoscalingv2.PodsScalingPolicy,
					Value:         1,
					PeriodSeconds: 60,
				},
			},
		},
		ScaleUp: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &StabilizationWindowSeconds,
			Policies: []autoscalingv2.HPAScalingPolicy{
				{
					Type:          autoscalingv2.PodsScalingPolicy,
					Value:         1,
					PeriodSeconds: 60,
				},
			},
		},
	}
	hpa := CreateTestHPA(&minReplicas, maxReplicas, quantity, behaviour, conditions)
	hpa.Status.CurrentReplicas = 0

	deploy := CreateTestDeploy(&replicas)

	fakeKubeClient := fake.NewClientset(deploy)
	fakeExternalMetrics := &fakeexternalmetrics.FakeExternalMetricsClient{}
	fakeCustomMetrics := &fakecustommetrics.FakeCustomMetricsClient{}

	// Add external metric using AddReactor - this mocks the external metrics API
	fakeExternalMetrics.Fake.AddReactor("list", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Return a metric list with the desired metric value
		return true, &v1beta1.ExternalMetricValueList{
			Items: []v1beta1.ExternalMetricValue{
				{
					MetricName: "scale_to_zero_demo_replica_count",
					Value:      *resource.NewQuantity(1, resource.DecimalSI), // This is the metric value that will be returned
					Timestamp:  metav1.Now(),
				},
			},
		}, nil
	})

	logger := logr.Discard()

	ctx := hpaScopedContext{
		Context:               context.TODO(),
		hpa:                   hpa,
		logger:                &logger,
		kubeClient:            fakeKubeClient,
		customMetricsClient:   fakeCustomMetrics,
		externalMetricsClient: fakeExternalMetrics,
	}

	// Execute the function
	err := actualizeHpaTargetState(ctx)

	// Verify results
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}

	// Check that a patch action was called to scale to 0
	actions := fakeKubeClient.Actions()
	if len(actions) > 0 {
		t.Error("Expected no actions, but some were found")
		t.Fail()
	}

}

func TestScalingUpWithBehaviourAllowed(t *testing.T) {

	replicas := int32(0)
	minReplicas := int32(1)
	maxReplicas := int32(10)
	quantity := int64(1)
	StabilizationWindowSeconds := int32(60)
	conditions := []autoscalingv2.HorizontalPodAutoscalerCondition{
		{
			Type:               autoscalingv2.ScalingActive,
			Status:             "False",
			LastTransitionTime: metav1.NewTime(time.Now().Add(-61 * time.Second)),
			Reason:             "ScalingDisabled",
			Message:            "scaling target replicas = 0",
		}}
	behaviour := &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &StabilizationWindowSeconds,
			Policies: []autoscalingv2.HPAScalingPolicy{
				{
					Type:          autoscalingv2.PodsScalingPolicy,
					Value:         1,
					PeriodSeconds: 60,
				},
			},
		},
		ScaleUp: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &StabilizationWindowSeconds,
			Policies: []autoscalingv2.HPAScalingPolicy{
				{
					Type:          autoscalingv2.PodsScalingPolicy,
					Value:         1,
					PeriodSeconds: 60,
				},
			},
		},
	}
	hpa := CreateTestHPA(&minReplicas, maxReplicas, quantity, behaviour, conditions)
	hpa.Status.CurrentReplicas = 0

	deploy := CreateTestDeploy(&replicas)

	fakeKubeClient := fake.NewClientset(deploy)
	fakeExternalMetrics := &fakeexternalmetrics.FakeExternalMetricsClient{}
	fakeCustomMetrics := &fakecustommetrics.FakeCustomMetricsClient{}

	// Add external metric using AddReactor - this mocks the external metrics API
	fakeExternalMetrics.Fake.AddReactor("list", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Return a metric list with the desired metric value
		return true, &v1beta1.ExternalMetricValueList{
			Items: []v1beta1.ExternalMetricValue{
				{
					MetricName: "scale_to_zero_demo_replica_count",
					Value:      *resource.NewQuantity(1, resource.DecimalSI), // This is the metric value that will be returned
					Timestamp:  metav1.Now(),
				},
			},
		}, nil
	})

	logger := logr.Discard()

	ctx := hpaScopedContext{
		Context:               context.TODO(),
		hpa:                   hpa,
		logger:                &logger,
		kubeClient:            fakeKubeClient,
		customMetricsClient:   fakeCustomMetrics,
		externalMetricsClient: fakeExternalMetrics,
	}

	// Execute the function
	err := actualizeHpaTargetState(ctx)

	// Verify results
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}

	// Check that a patch action was called to scale to 0
	actions := fakeKubeClient.Actions()
	found := false
	for _, action := range actions {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "deployments" {
			// Cast the action to PatchAction to get patch details
			if patchAction, ok := action.(k8stesting.PatchAction); ok {
				t.Logf("Patch action found with data: %s", string(patchAction.GetPatch()))
			}
			found = true

			// Get the deployment to verify it was scaled to 1
			deployment, err := fakeKubeClient.AppsV1().Deployments("default").Get(context.TODO(), "test-deployment", metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get deployment: %v", err)
			} else if *deployment.Spec.Replicas != 1 {
				t.Errorf("Expected deployment to be scaled to 1, but got %d replicas", *deployment.Spec.Replicas)
			}
			break
		}
	}
	if !found {
		t.Error("Expected deployment patch action to scale to 0, but none was found")
	}
}
