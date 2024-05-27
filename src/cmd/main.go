package main

import (
	"fmt"
	"github.com/SPSCommerce/kube-hpa-scale-to-zero/internal"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/custom_metrics"
	"k8s.io/metrics/pkg/client/external_metrics"
	"net/http"
	"time"
)

func UpEndpointHandler(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte("happy"))
	if err != nil {
		zap.Error(fmt.Errorf("unable to handle request to up endpoint: %s", err))
	}
}

func main() {

	runtimeConfig := internal.LoadConfiguration()

	logger := internal.NewLogger("info", runtimeConfig.WritePlainLogs)
	defer logger.Sync()

	logger.Infof("Starting with configuration %s", runtimeConfig)

	config, err := clientcmd.BuildConfigFromFlags("", runtimeConfig.KubeConfigPath)
	if err != nil {
		logger.Fatalf("Unable to create kubernetes config: %s", err)
	}

	client := kubernetes.NewForConfigOrDie(config)

	discoveryClient, err := disk.NewCachedDiscoveryClientForConfig(config, ".discoveryCache", ".httpCache",
		time.Hour)

	if err != nil {
		logger.Fatalf("Unable to create kubernetes discovery client: %s", err)
	}

	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)

	customMetricsClient := custom_metrics.NewForConfig(config, restMapper, custom_metrics.NewAvailableAPIsGetter(discoveryClient))

	externalMetricsClient := external_metrics.NewForConfigOrDie(config)

	metricsContext := internal.RegisterMetricsContext("scale_to_zero")

	rootCtx := context.Background()
	ctx, _ := context.WithCancel(rootCtx)

	go internal.SetupHpaInformer(ctx, logger.Named("Informer"), client, customMetricsClient, externalMetricsClient, &metricsContext, runtimeConfig.HpaSelector)

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/up", http.HandlerFunc(UpEndpointHandler))
	if http.ListenAndServe(fmt.Sprintf(":%d", runtimeConfig.Port), nil) != nil {
		logger.Fatalf("unable to listen port %d: %s", runtimeConfig.Port, err)
	}
}
