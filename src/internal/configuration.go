package internal

import (
	"flag"
	"fmt"
)

type RunConfiguration struct {
	KubeConfigPath string
	HpaSelector    string
	Port           int
	WritePlainLogs bool
}

func LoadConfiguration() *RunConfiguration {
	config := &RunConfiguration{
		KubeConfigPath: "",
		HpaSelector:    "scaletozero.spscommerce.com/watch",
		Port:           443,
		WritePlainLogs: false,
	}

	// how to run service itself
	flag.StringVar(&config.KubeConfigPath, "kube-config", config.KubeConfigPath, "(optional) kube config to use when ")
	flag.BoolVar(&config.WritePlainLogs, "use-plain-logs", config.WritePlainLogs, fmt.Sprintf("(optional) turn on plain logs"))
	flag.IntVar(&config.Port, "port", config.Port, "(optional) app port")
	flag.StringVar(&config.HpaSelector, "hpa-selector", config.HpaSelector, "(optional) hpa label to watch for")

	flag.Parse()

	return config
}
