package main

import (
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	cbs "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/regions"
	es "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/es/v20180416"
	"gopkg.in/alecthomas/kingpin.v2"
	"net/http"
	"os"
)

const (
	NameSpace = "tc_info"
)

type Exporter struct {
	logger    log.Logger
	rateLimit int

	esInstance  *prometheus.Desc
	cbsInstance *prometheus.Desc
}

func NewExporter(rateLimit int, logger log.Logger) *Exporter {
	return &Exporter{
		logger:    logger,
		rateLimit: rateLimit,
		esInstance: prometheus.NewDesc(
			prometheus.BuildFQName(NameSpace, "es", "instance"),
			"elastic instance on tencent cloud",
			[]string{"instance_id", "name", "es_version"},
			nil,
		),

		cbsInstance: prometheus.NewDesc(
			prometheus.BuildFQName(NameSpace, "cbs", "instance"),
			"cbs instance on tencent cloud",
			[]string{"instance_id", "disk_id", "type", "name", "state"},
			nil,
		),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.esInstance
	ch <- e.cbsInstance
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	// 连接腾讯云
	provider := common.DefaultEnvProvider()
	credential, err := provider.GetCredential()
	if err != nil {
		_ = level.Error(e.logger).Log("msg", "Failed to get credential")
		panic(err)
	}
	// es collect
	esClient, err := es.NewClient(credential, regions.Beijing, profile.NewClientProfile())
	if err != nil {
		_ = level.Error(e.logger).Log("msg", "Failed to get tencent client")
		panic(err)
	}

	esRequest := es.NewDescribeInstancesRequest()
	esResponse, err := esClient.DescribeInstances(esRequest)

	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		fmt.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		panic(err)
	}
	// 暴露指标
	for _, ins := range esResponse.Response.InstanceList {
		ch <- prometheus.MustNewConstMetric(e.esInstance, prometheus.GaugeValue, 1,
			[]string{*ins.InstanceId, *ins.InstanceName, *ins.EsVersion}...)
	}

	// cbs collect
	cbsClient, err := cbs.NewClient(credential, regions.Beijing, profile.NewClientProfile())
	if err != nil {
		_ = level.Error(e.logger).Log("msg", "Failed to get tencent client")
		panic(err)
	}
	cbsRequest := cbs.NewDescribeDisksRequest()
	cbsRequest.Limit = common.Uint64Ptr(100)
	cbsResponse, err := cbsClient.DescribeDisks(cbsRequest)

	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		fmt.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		panic(err)
	}
	cbsTotal := *cbsResponse.Response.TotalCount
	var count = uint64(0)
	for {
		if count > cbsTotal {
			break
		}
		for _, disk := range cbsResponse.Response.DiskSet {
			ch <- prometheus.MustNewConstMetric(e.cbsInstance, prometheus.GaugeValue, 1,
				[]string{*disk.InstanceId, *disk.DiskId, *disk.InstanceType, *disk.DiskName, *disk.DiskState}...)
		}
		count += 100
		cbsRequest.Offset = common.Uint64Ptr(count)
		cbsResponse, err = cbsClient.DescribeDisks(cbsRequest)
	}
}

func main() {
	var (
		webConfig     = webflag.AddFlags(kingpin.CommandLine)
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9150").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	)
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Version(version.Print("tc_info_exporter"))
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	err := godotenv.Load()
	if err != nil {
		_ = level.Warn(logger).Log("msg", "Error loading .env file")
	}

	_ = level.Info(logger).Log("msg", "Starting tc_info_exporter", "version", version.Info())
	_ = level.Info(logger).Log("msg", "Build context", "context", version.BuildContext())

	prometheus.MustRegister(version.NewCollector(NameSpace))
	prometheus.MustRegister(NewExporter(15, logger))

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>
             <head><title>Tecent cloud info Exporter</title></head>
             <body>
             <h1>Tencent cloud info exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	_ = level.Info(logger).Log("msg", "Listening on address", "address", *listenAddress)
	srv := &http.Server{Addr: *listenAddress}
	if err := web.ListenAndServe(srv, *webConfig, logger); err != nil {
		_ = level.Error(logger).Log("msg", "Error running HTTP server", "err", err)
		os.Exit(1)
	}
}
