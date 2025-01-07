package main

import (
	"github.com/dddfiish/tencentcloud-info-exporter/pkg/collector"
	"github.com/dddfiish/tencentcloud-info-exporter/pkg/config"
	"github.com/go-kit/log/level"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	cbs "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/regions"
	"gopkg.in/alecthomas/kingpin.v2"
	"net/http"
	"os"
)

func main() {
	var (
		webConfig     = webflag.AddFlags(kingpin.CommandLine)
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9150").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		configFile    = kingpin.Flag("config.file", "Tencent qcloud exporter configuration file.").Default("cdn.yaml").String()
		enableEs      = kingpin.Flag("metrics.es", "Enable metric es").Bool()
		enableCbs     = kingpin.Flag("metrics.cbs", "Enable metric cbs").Bool()
		enableCDN     = kingpin.Flag("metrics.cdn", "Enable metric cdn").Default("true").Bool()
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

	tencentConfig := &config.TencentConfig{}
	// load config file
	if err := tencentConfig.LoadFile(*configFile); err != nil {
		_ = level.Error(logger).Log("msg", "Load config error", "err", err)
		os.Exit(1)
	} else {
		_ = level.Info(logger).Log("msg", "Load config ok")
	}

	err := godotenv.Load()
	if err != nil {
		_ = level.Warn(logger).Log("msg", "Error loading .env file")
	}

	_ = level.Info(logger).Log("msg", "Starting tc_info_exporter", "version", version.Info())
	_ = level.Info(logger).Log("msg", "Build context", "context", version.BuildContext())

	// connect to tencent cloud
	provider := common.DefaultEnvProvider()
	credential, err := provider.GetCredential()
	if err != nil {
		_ = level.Error(logger).Log("msg", "Failed to get credential")
		panic(err)
	}

	prometheus.MustRegister(version.NewCollector(config.NameSpace))
	prometheus.Unregister(collectors.NewGoCollector())
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	if *enableCbs {
		cpf := profile.NewClientProfile()
		cpf.Debug = *debug
		cpf.HttpProfile.ReqTimeout = *timeout
		cbsClient, err := cbs.NewClient(credential, regions.Beijing, cpf)
		if err != nil {
			_ = level.Error(logger).Log("msg", "Failed to get tencent client")
			panic(err)
		}
		prometheus.MustRegister(collector.NewCbsExporter(15, logger, *cbsPageLimit, cbsClient))
	}

	if *enableEs {
		prometheus.MustRegister(collector.NewEsExporter(15, logger, credential))
	}

	if *enableCDN {
		prometheus.MustRegister(collector.NewCdnExporter(20, logger, credential, *tencentConfig))
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
