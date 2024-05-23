package internal

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
