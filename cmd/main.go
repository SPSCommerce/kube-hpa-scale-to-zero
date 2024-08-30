package main

import (
	"flag"
	"fmt"
	"github.com/SPSCommerce/kube-hpa-scale-to-zero/internal"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap/zapcore"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/custom_metrics"
	"k8s.io/metrics/pkg/client/external_metrics"
	"net/http"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"time"
)

type RunConfiguration struct {
	KubeConfigPath    string
	HpaSelector       string
	Port              int
	IsDevelopmentMode bool
}

func loadConfiguration() *RunConfiguration {
	config := &RunConfiguration{
		KubeConfigPath:    "",
		HpaSelector:       "scaletozero.spscommerce.com/watch",
		Port:              443,
		IsDevelopmentMode: false,
	}

	// how to run service itself
	flag.StringVar(&config.KubeConfigPath, "kube-config", config.KubeConfigPath, "(optional) kube config to use when ")
	flag.BoolVar(&config.IsDevelopmentMode, "development-mode", config.IsDevelopmentMode, fmt.Sprintf("(optional) turn on development mode for logs"))
	flag.IntVar(&config.Port, "port", config.Port, "(optional) app port")
	flag.StringVar(&config.HpaSelector, "hpa-selector", config.HpaSelector, "(optional) hpa label to watch for")

	flag.Parse()

	return config
}

func UpEndpointHandler(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte("happy"))
	if err != nil {
		ctrl.Log.Error(err, "unable to handle request to up endpoint")
	}
}

func main() {

	runtimeConfig := loadConfiguration()

	opts := zap.Options{
		Development: runtimeConfig.IsDevelopmentMode,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logger := zap.New(zap.UseFlagOptions(&opts))

	ctrl.SetLogger(logger)

	jsonConfig, _ := json.Marshal(runtimeConfig)

	logger.Info(fmt.Sprintf("starting with configuration %s", jsonConfig))

	config, err := clientcmd.BuildConfigFromFlags("", runtimeConfig.KubeConfigPath)
	if err != nil {
		logger.Error(err, "unable to create kubernetes config")
		os.Exit(1)
	}

	client := kubernetes.NewForConfigOrDie(config)

	discoveryClient, err := disk.NewCachedDiscoveryClientForConfig(config, ".discoveryCache", ".httpCache",
		time.Second)

	if err != nil {
		logger.Error(err, "unable to create kubernetes discovery client")
		os.Exit(1)
	}

	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)

	apiGetter := custom_metrics.NewAvailableAPIsGetter(discoveryClient)
	_, err = apiGetter.PreferredVersion()
	if err != nil {
		logger.Error(err, "unable to discover custom metrics api")
		os.Exit(1)
	}
	customMetricsClient := custom_metrics.NewForConfig(config, restMapper, custom_metrics.NewAvailableAPIsGetter(discoveryClient))

	externalMetricsClient := external_metrics.NewForConfigOrDie(config)

	ctx := context.Background()

	informerLog := logger.WithName("Informer")
	go internal.SetupHpaInformer(ctx, &informerLog, client, customMetricsClient, externalMetricsClient, runtimeConfig.HpaSelector)

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/up", http.HandlerFunc(UpEndpointHandler))
	if http.ListenAndServe(fmt.Sprintf(":%d", runtimeConfig.Port), nil) != nil {
		logger.Error(err, fmt.Sprintf("unable to listen port %d", runtimeConfig.Port))
	}
}
