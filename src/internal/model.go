package internal

type MetricValueList struct {
	Kind       string `json:"kind"`
	ApiVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []struct {
		DescribedObject struct {
			Kind       string `json:"kind"`
			Namespace  string `json:"namespace"`
			Name       string `json:"name"`
			ApiVersion string `json:"apiVersion"`
		} `json:"describedObject"`
		MetricName string `json:"metricName"`
		Timestamp  string `json:"timestamp"`
		Value      string `json:"value"`
		Selector   struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
	} `json:"items"`
}

type DeploymentReplicasSpec struct {
	Replicas int `json:"replicas"`
}

type DeploymentReplicasPatch struct {
	Spec DeploymentReplicasSpec `json:"spec"`
}

type MetricValue struct {
	IsZero bool
	Error  error
}

type MetricsState int

const (
	AllMetricsAreZero = iota
	SomeMetricIsNotZero
)
