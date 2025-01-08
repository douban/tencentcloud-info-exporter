package main

import (
	"fmt"
	"github.com/LeoQuote/tencentcloud-info-exporter/pkg/collector"
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

type EsExporter struct {
	logger     log.Logger
	rateLimit  int
	credential common.CredentialIface

	esInstance *prometheus.Desc
}

func NewEsExporter(rateLimit int, logger log.Logger, credential common.CredentialIface) *EsExporter {
	return &EsExporter{
		logger:     logger,
		rateLimit:  rateLimit,
		credential: credential,

		esInstance: prometheus.NewDesc(
			prometheus.BuildFQName(NameSpace, "es", "instance"),
			"elastic instance on tencent cloud",
			[]string{"instance_id", "name", "es_version"},
			nil,
		),
	}
}

func (e *EsExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.esInstance
}

func (e *EsExporter) Collect(ch chan<- prometheus.Metric) {
	// es collect
	esClient, err := es.NewClient(e.credential, regions.Beijing, profile.NewClientProfile())
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
}

type CbsExporter struct {
	logger    log.Logger
	rateLimit int
	pageLimit uint64
	client    *cbs.Client

	cbsInstance *prometheus.Desc
}

func NewCbsExporter(rateLimit int, logger log.Logger, pageLimit uint64, client *cbs.Client) *CbsExporter {
	return &CbsExporter{
		logger:    logger,
		rateLimit: rateLimit,
		pageLimit: pageLimit,
		client:    client,

		cbsInstance: prometheus.NewDesc(
			prometheus.BuildFQName(NameSpace, "cbs", "instance"),
			"cbs instance on tencent cloud",
			[]string{"instance_id", "disk_id", "type", "name", "state"},
			nil,
		),
	}
}

func (e *CbsExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.cbsInstance
}

func (e *CbsExporter) Collect(ch chan<- prometheus.Metric) {
	// cbs collect
	cbsRequest := cbs.NewDescribeDisksRequest()
	cbsRequest.Limit = common.Uint64Ptr(e.pageLimit)
	cbsResponse, err := e.client.DescribeDisks(cbsRequest)

	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		fmt.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		if cbsResponse != nil {
			_ = level.Error(e.logger).Log("error", "The API request id", *cbsResponse.Response.RequestId)
		}
		panic(err)
	}
	_ = level.Debug(e.logger).Log("msg", "request success, request id: ", cbsResponse.Response.RequestId)
	cbsTotal := *cbsResponse.Response.TotalCount
	var count = uint64(0)
	for {
		if count > cbsTotal {
			break
		}
		cbsResponse, err = e.client.DescribeDisks(cbsRequest)
		if err != nil {
			var retry = 3
			for {
				if retry == 0 {
					break
				}
				cbsResponse, err = e.client.DescribeDisks(cbsRequest)
				if err == nil {
					break
				}
				retry--
			}
		}
		if err != nil {
			if cbsResponse != nil {
				fmt.Printf("The API request id: %s ", *cbsResponse.Response.RequestId)
			}
			panic(err)
		}
		_ = level.Debug(e.logger).Log("msg", "request success, request id: %s", cbsResponse.Response.RequestId)
		for _, disk := range cbsResponse.Response.DiskSet {
			ch <- prometheus.MustNewConstMetric(e.cbsInstance, prometheus.GaugeValue, 1,
				[]string{*disk.InstanceId, *disk.DiskId, *disk.InstanceType, *disk.DiskName, *disk.DiskState}...)
		}
		count += e.pageLimit
		cbsRequest.Offset = common.Uint64Ptr(count)
	}
}

func main() {
	var (
		webConfig     = webflag.AddFlags(kingpin.CommandLine)
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9150").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		enableEs      = kingpin.Flag("metrics.es", "Enable metric es").Bool()
		enableCbs     = kingpin.Flag("metrics.cbs", "Enable metric cbs").Bool()
		enableCDN     = kingpin.Flag("metrics.cdn", "Enable metric cdn").Bool()
		delaySeconds  = kingpin.Flag("delay-seconds", "Delay in seconds").Default("360").Int()
		Domains       = kingpin.Flag("domains", "domains").Strings()
		cbsPageLimit  = kingpin.Flag("cbs.page-limit", "CBS page limit, max 100").Default("100").Uint64()
		debug         = kingpin.Flag("debug", "Enable debug log").Default("false").Bool()
		timeout       = kingpin.Flag("timeout", "SDK timeout").Default("30").Int()
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

	// 连接腾讯云
	provider := common.DefaultEnvProvider()
	credential, err := provider.GetCredential()
	if err != nil {
		_ = level.Error(logger).Log("msg", "Failed to get credential")
		panic(err)
	}

	prometheus.MustRegister(version.NewCollector(NameSpace))
	if *enableCbs {
		cpf := profile.NewClientProfile()
		cpf.Debug = *debug
		cpf.HttpProfile.ReqTimeout = *timeout
		cbsClient, err := cbs.NewClient(credential, regions.Beijing, cpf)
		if err != nil {
			_ = level.Error(logger).Log("msg", "Failed to get tencent client")
			panic(err)
		}
		prometheus.MustRegister(NewCbsExporter(15, logger, *cbsPageLimit, cbsClient))
	}
	if *enableEs {
		prometheus.MustRegister(NewEsExporter(15, logger, credential))
	}
	if *enableCDN {
		prometheus.MustRegister(collector.NewCdnExporter(15, *delaySeconds, *Domains, logger, credential))
	}

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
