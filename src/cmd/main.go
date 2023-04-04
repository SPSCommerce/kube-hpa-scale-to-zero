package main

import (
	"fmt"
	"github.com/SPSCommerce/kube-hpa-scale-to-zero/internal"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/net/context"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/custom_metrics"
	"k8s.io/metrics/pkg/client/external_metrics"
	"net/http"
	"os"
	"strconv"
	"time"
)

type RunConfiguration struct {
	KubeConfigPath string
	HpaSelector    string
	Port           int
	WritePlainLogs bool
}

func ParseInputArguments(arguments []string) (*RunConfiguration, error) {

	result := RunConfiguration{
		KubeConfigPath: "",
		HpaSelector:    "",
		Port:           8080,
		WritePlainLogs: false,
	}

	currentPosition := 0

	for currentPosition < len(arguments) {

		currentArgument := arguments[currentPosition]

		if currentArgument == "--kube-config" {
			result.KubeConfigPath = arguments[currentPosition+1]
			currentPosition += 1
		} else if currentArgument == "--hpa-selector" {
			result.HpaSelector = arguments[currentPosition+1]
			currentPosition += 1
		} else if currentArgument == "--port" {
			port, err := strconv.Atoi(arguments[currentPosition+1])
			if err != nil {
				return nil, fmt.Errorf("port has to be int, got '%s' instead", arguments[currentPosition+1])
			}
			result.Port = port
			currentPosition += 1
		} else if currentArgument == "--write-plain-logs" {
			result.WritePlainLogs = true
		} else {
			return nil, fmt.Errorf("unknown argument '%s'", currentArgument)
		}

		currentPosition += 1
	}

	return &result, nil
}

func getLogEncoder(usePlain bool, config zapcore.EncoderConfig) zapcore.Encoder {
	if usePlain {
		return zapcore.NewConsoleEncoder(config)
	}

	return zapcore.NewJSONEncoder(config)
}

func UpEndpointHandler(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte("happy"))
	if err != nil {
		zap.Error(fmt.Errorf("unable to handle request to up endpoint: %s", err))
	}
}

func main() {

	runtimeConfig, err := ParseInputArguments(os.Args[1:])
	if err != nil {
		panic(err)
	}

	infoLevel := zap.LevelEnablerFunc(func(level zapcore.Level) bool {
		return level <= zapcore.WarnLevel
	})

	errorLevel := zap.LevelEnablerFunc(func(level zapcore.Level) bool {
		return level > zapcore.WarnLevel
	})

	stdoutSyncer := zapcore.Lock(os.Stdout)
	stderrSyncer := zapcore.Lock(os.Stderr)

	core := zapcore.NewTee(
		zapcore.NewCore(
			getLogEncoder(runtimeConfig.WritePlainLogs, zap.NewProductionEncoderConfig()),
			stdoutSyncer,
			infoLevel,
		),
		zapcore.NewCore(
			getLogEncoder(runtimeConfig.WritePlainLogs, zap.NewProductionEncoderConfig()),
			stderrSyncer,
			errorLevel,
		),
	)

	logger := zap.New(core).Named("Root").Sugar()
	defer logger.Sync()

	logger.Infof("Starting with configuration %s", runtimeConfig)

	if runtimeConfig.HpaSelector == "" {
		logger.Fatalf("HPA selector was not specified, please specify `--hpa-selector` param")
	}

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

	go internal.SetupHpaInformer(ctx, logger.Named("Informer"), client, customMetricsClient, externalMetricsClient, metricsContext, runtimeConfig.HpaSelector)

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/up", http.HandlerFunc(UpEndpointHandler))
	if http.ListenAndServe(fmt.Sprintf(":%d", runtimeConfig.Port), nil) != nil {
		logger.Fatalf("unable to listen port %d: %s", runtimeConfig.Port, err)
	}
}
