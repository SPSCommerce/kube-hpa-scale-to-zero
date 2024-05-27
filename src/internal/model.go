package internal

type DeploymentReplicasSpec struct {
	Replicas int `json:"replicas"`
}

type DeploymentReplicasPatch struct {
	Spec DeploymentReplicasSpec `json:"spec"`
}
