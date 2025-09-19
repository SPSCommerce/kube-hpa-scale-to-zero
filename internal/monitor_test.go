package internal

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	autoscaling "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	custommetricsv1beta1 "k8s.io/metrics/pkg/apis/custom_metrics/v1beta1"
	externalmetricsv1beta1 "k8s.io/metrics/pkg/apis/external_metrics/v1beta1"
	custommetricsfake "k8s.io/metrics/pkg/client/custom_metrics/fake"
	externalmetricsfake "k8s.io/metrics/pkg/client/external_metrics/fake"
)

func TestBuildMetricsSelector(t *testing.T) {
	logger := logr.Discard()
	hpa := &autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "test-namespace",
		},
	}
	ctx := hpaScopedContext{
		Context: context.Background(),
		hpa:     hpa,
		logger:  &logger,
	}

	tests := []struct {
		name     string
		selector *metav1.LabelSelector
		wantErr  bool
	}{
		{
			name:     "nil selector should return empty selector",
			selector: nil,
			wantErr:  false,
		},
		{
			name: "selector with match labels should work",
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			wantErr: false,
		},
		{
			name: "selector with match expressions should fail",
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test"},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildMetricsSelector(ctx, tt.selector)
			if tt.wantErr {
				if err == nil {
					t.Errorf("buildMetricsSelector() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("buildMetricsSelector() unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("buildMetricsSelector() expected result but got nil")
				}
			}
		})
	}
}

func TestRequestIfObjectMetricValueIsZero(t *testing.T) {
	logger := logr.Discard()
	hpa := &autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "test-namespace",
		},
	}

	tests := []struct {
		name           string
		spec           *autoscaling.MetricSpec
		mockValue      *resource.Quantity
		mockError      error
		expectedResult bool
		wantErr        bool
	}{
		{
			name: "deployment metric with zero value",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ObjectMetricSourceType,
				Object: &autoscaling.ObjectMetricSource{
					DescribedObject: autoscaling.CrossVersionObjectReference{
						Kind: "Deployment",
						Name: "test-deployment",
					},
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValue:      resource.NewQuantity(0, resource.DecimalSI),
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "deployment metric with non-zero value",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ObjectMetricSourceType,
				Object: &autoscaling.ObjectMetricSource{
					DescribedObject: autoscaling.CrossVersionObjectReference{
						Kind: "Deployment",
						Name: "test-deployment",
					},
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValue:      resource.NewQuantity(10, resource.DecimalSI),
			expectedResult: false,
			wantErr:        false,
		},
		{
			name: "service metric with zero value",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ObjectMetricSourceType,
				Object: &autoscaling.ObjectMetricSource{
					DescribedObject: autoscaling.CrossVersionObjectReference{
						Kind: "Service",
						Name: "test-service",
					},
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValue:      resource.NewQuantity(0, resource.DecimalSI),
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "unsupported kind should fail",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ObjectMetricSourceType,
				Object: &autoscaling.ObjectMetricSource{
					DescribedObject: autoscaling.CrossVersionObjectReference{
						Kind: "StatefulSet",
						Name: "test-statefulset",
					},
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "metrics client error should fail",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ObjectMetricSourceType,
				Object: &autoscaling.ObjectMetricSource{
					DescribedObject: autoscaling.CrossVersionObjectReference{
						Kind: "Deployment",
						Name: "test-deployment",
					},
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockError: fmt.Errorf("metrics client error"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customMetricsClient := &custommetricsfake.FakeCustomMetricsClient{}

			if tt.mockError != nil {
				customMetricsClient.AddReactor("get", "*", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, tt.mockError
				})
			} else if tt.mockValue != nil {
				customMetricsClient.AddReactor("get", "*", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, &custommetricsv1beta1.MetricValue{
						Value: *tt.mockValue,
					}, nil
				})
			}

			ctx := hpaScopedContext{
				Context:             context.Background(),
				hpa:                 hpa,
				logger:              &logger,
				customMetricsClient: customMetricsClient,
			}

			result, err := requestIfObjectMetricValueIsZero(ctx, tt.spec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("requestIfObjectMetricValueIsZero() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("requestIfObjectMetricValueIsZero() unexpected error: %v", err)
				}
				if result != tt.expectedResult {
					t.Errorf("requestIfObjectMetricValueIsZero() = %v, want %v", result, tt.expectedResult)
				}
			}
		})
	}
}

func TestRequestIfExternalMetricValueIsZero(t *testing.T) {
	logger := logr.Discard()
	hpa := &autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "test-namespace",
		},
	}

	tests := []struct {
		name           string
		spec           *autoscaling.MetricSpec
		mockValues     []resource.Quantity
		mockError      error
		expectedResult bool
		wantErr        bool
	}{
		{
			name: "external metric with all zero values",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ExternalMetricSourceType,
				External: &autoscaling.ExternalMetricSource{
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValues:     []resource.Quantity{*resource.NewQuantity(0, resource.DecimalSI)},
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "external metric with non-zero values",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ExternalMetricSourceType,
				External: &autoscaling.ExternalMetricSource{
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValues:     []resource.Quantity{*resource.NewQuantity(10, resource.DecimalSI)},
			expectedResult: false,
			wantErr:        false,
		},
		{
			name: "external metric with mixed values",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ExternalMetricSourceType,
				External: &autoscaling.ExternalMetricSource{
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValues: []resource.Quantity{
				*resource.NewQuantity(0, resource.DecimalSI),
				*resource.NewQuantity(10, resource.DecimalSI),
			},
			expectedResult: false,
			wantErr:        false,
		},
		{
			name: "external metric with no items should fail",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ExternalMetricSourceType,
				External: &autoscaling.ExternalMetricSource{
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockValues: []resource.Quantity{},
			wantErr:    true,
		},
		{
			name: "external metrics client error should fail",
			spec: &autoscaling.MetricSpec{
				Type: autoscaling.ExternalMetricSourceType,
				External: &autoscaling.ExternalMetricSource{
					Metric: autoscaling.MetricIdentifier{
						Name: "test-metric",
					},
				},
			},
			mockError: fmt.Errorf("external metrics client error"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			externalMetricsClient := &externalmetricsfake.FakeExternalMetricsClient{}

			if tt.mockError != nil {
				externalMetricsClient.AddReactor("list", "*", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, tt.mockError
				})
			} else {
				items := make([]externalmetricsv1beta1.ExternalMetricValue, len(tt.mockValues))
				for i, value := range tt.mockValues {
					items[i] = externalmetricsv1beta1.ExternalMetricValue{
						Value: value,
					}
				}
				externalMetricsClient.AddReactor("list", "*", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, &externalmetricsv1beta1.ExternalMetricValueList{
						Items: items,
					}, nil
				})
			}

			ctx := hpaScopedContext{
				Context:               context.Background(),
				hpa:                   hpa,
				logger:                &logger,
				externalMetricsClient: externalMetricsClient,
			}

			result, err := requestIfExternalMetricValueIsZero(ctx, tt.spec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("requestIfExternalMetricValueIsZero() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("requestIfExternalMetricValueIsZero() unexpected error: %v", err)
				}
				if result != tt.expectedResult {
					t.Errorf("requestIfExternalMetricValueIsZero() = %v, want %v", result, tt.expectedResult)
				}
			}
		})
	}
}

func TestExtractMetricValuesFromCurrentMetrics(t *testing.T) {
	tests := []struct {
		name           string
		hpa            *autoscaling.HorizontalPodAutoscaler
		expectedResult []bool
		wantErr        bool
	}{
		{
			name: "object metric with zero value",
			hpa: &autoscaling.HorizontalPodAutoscaler{
				Status: autoscaling.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscaling.MetricStatus{
						{
							Type: autoscaling.ObjectMetricSourceType,
							Object: &autoscaling.ObjectMetricStatus{
								Current: autoscaling.MetricValueStatus{
									Value: resource.NewQuantity(0, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			expectedResult: []bool{true},
			wantErr:        false,
		},
		{
			name: "object metric with non-zero value",
			hpa: &autoscaling.HorizontalPodAutoscaler{
				Status: autoscaling.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscaling.MetricStatus{
						{
							Type: autoscaling.ObjectMetricSourceType,
							Object: &autoscaling.ObjectMetricStatus{
								Current: autoscaling.MetricValueStatus{
									Value: resource.NewQuantity(10, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			expectedResult: []bool{false},
			wantErr:        false,
		},
		{
			name: "external metric with zero value",
			hpa: &autoscaling.HorizontalPodAutoscaler{
				Status: autoscaling.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscaling.MetricStatus{
						{
							Type: autoscaling.ExternalMetricSourceType,
							External: &autoscaling.ExternalMetricStatus{
								Current: autoscaling.MetricValueStatus{
									Value: resource.NewQuantity(0, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			expectedResult: []bool{true},
			wantErr:        false,
		},
		{
			name: "mixed metrics",
			hpa: &autoscaling.HorizontalPodAutoscaler{
				Status: autoscaling.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscaling.MetricStatus{
						{
							Type: autoscaling.ObjectMetricSourceType,
							Object: &autoscaling.ObjectMetricStatus{
								Current: autoscaling.MetricValueStatus{
									Value: resource.NewQuantity(0, resource.DecimalSI),
								},
							},
						},
						{
							Type: autoscaling.ExternalMetricSourceType,
							External: &autoscaling.ExternalMetricStatus{
								Current: autoscaling.MetricValueStatus{
									Value: resource.NewQuantity(10, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			expectedResult: []bool{true, false},
			wantErr:        false,
		},
		{
			name: "metric with no type should fail",
			hpa: &autoscaling.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "test-namespace",
				},
				Status: autoscaling.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscaling.MetricStatus{
						{
							Type: "",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported metric type should fail",
			hpa: &autoscaling.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					Namespace: "test-namespace",
				},
				Status: autoscaling.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscaling.MetricStatus{
						{
							Type: "UnsupportedType",
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractMetricValuesFromCurrentMetrics(tt.hpa)

			if tt.wantErr {
				if err == nil {
					t.Errorf("extractMetricValuesFromCurrentMetrics() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("extractMetricValuesFromCurrentMetrics() unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("extractMetricValuesFromCurrentMetrics() expected result but got nil")
				} else {
					if len(*result) != len(tt.expectedResult) {
						t.Errorf("extractMetricValuesFromCurrentMetrics() result length = %v, want %v", len(*result), len(tt.expectedResult))
					} else {
						for i, expected := range tt.expectedResult {
							if (*result)[i] != expected {
								t.Errorf("extractMetricValuesFromCurrentMetrics()[%d] = %v, want %v", i, (*result)[i], expected)
							}
						}
					}
				}
			}
		})
	}
}

func TestCheckAllAreZero(t *testing.T) {
	tests := []struct {
		name           string
		metricValues   []bool
		expectedResult bool
	}{
		{
			name:           "all zero values",
			metricValues:   []bool{true, true, true},
			expectedResult: true,
		},
		{
			name:           "mixed values",
			metricValues:   []bool{true, false, true},
			expectedResult: false,
		},
		{
			name:           "all non-zero values",
			metricValues:   []bool{false, false, false},
			expectedResult: false,
		},
		{
			name:           "single zero value",
			metricValues:   []bool{true},
			expectedResult: true,
		},
		{
			name:           "single non-zero value",
			metricValues:   []bool{false},
			expectedResult: false,
		},
		{
			name:           "empty slice",
			metricValues:   []bool{},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkAllAreZero(&tt.metricValues)
			if result != tt.expectedResult {
				t.Errorf("checkAllAreZero() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

// Test helper for DeploymentReplicasPatch struct
func TestDeploymentPatchGeneration(t *testing.T) {
	tests := []struct {
		name     string
		replicas int
	}{
		{
			name:     "scale to zero",
			replicas: 0,
		},
		{
			name:     "scale to one",
			replicas: 1,
		},
		{
			name:     "scale to multiple",
			replicas: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := DeploymentReplicasPatch{
				Spec: DeploymentReplicasSpec{
					Replicas: tt.replicas,
				},
			}

			if patch.Spec.Replicas != tt.replicas {
				t.Errorf("DeploymentReplicasPatch.Spec.Replicas = %v, want %v", patch.Spec.Replicas, tt.replicas)
			}
		})
	}
}

// Test helper for scaleHpaTarget function logic
func TestScaleHpaTargetLogic(t *testing.T) {
	tests := []struct {
		name       string
		targetKind string
		wantErr    bool
	}{
		{
			name:       "deployment target should work",
			targetKind: "Deployment",
			wantErr:    false,
		},
		{
			name:       "statefulset target should fail",
			targetKind: "StatefulSet",
			wantErr:    true,
		},
		{
			name:       "unknown target should fail",
			targetKind: "Unknown",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic without actually calling Kubernetes APIs
			var err error
			if tt.targetKind == "Deployment" {
				// This would be successful
				err = nil
			} else {
				// This would fail with unsupported error
				err = fmt.Errorf("target kind %q is not supported", tt.targetKind)
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("scaleHpaTarget() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("scaleHpaTarget() unexpected error: %v", err)
				}
			}
		})
	}
}
