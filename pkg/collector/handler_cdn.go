package collector

import (
	"fmt"
	"github.com/dddfiish/tencentcloud-info-exporter/pkg/config"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	cdn "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdn/v20180606"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/regions"
	"math"
	"sync"
	"time"
)

type CdnExporter struct {
	logger        log.Logger
	rateLimit     int
	credential    common.CredentialIface
	tencentConfig config.TencentConfig

	cdnInstance *prometheus.Desc
}

func NewCdnExporter(rateLimit int, logger log.Logger, credential common.CredentialIface, tencentConfig config.TencentConfig) *CdnExporter {
	return &CdnExporter{
		logger:        logger,
		rateLimit:     rateLimit,
		credential:    credential,
		tencentConfig: tencentConfig,

		cdnInstance: prometheus.NewDesc(
			//douban_cdn_http_status_rate
			prometheus.BuildFQName(config.NameSpace, "cdn", "http_status_rate"),
			"CDN on Tencent Cloud",
			[]string{"domain", "provider", "status"},
			nil,
		),
	}
}

func (e *CdnExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.cdnInstance
}

func (e *CdnExporter) Collect(ch chan<- prometheus.Metric) {
	cdnClient, err := cdn.NewClient(e.credential, regions.Guangzhou, profile.NewClientProfile())
	if err != nil {
		_ = level.Error(e.logger).Log("msg", "Failed to get Tencent client")
		panic(err)
	}

	timeStr := time.Now().Add(-time.Duration(e.tencentConfig.DelaySeconds) * time.Second).Format("2006-01-02 15:04:05")

	var wg sync.WaitGroup

	for _, dimension := range e.tencentConfig.CustomQueryDimension {
		cdnRequest := cdn.NewDescribeCdnDataRequest()
		cdnRequest.StartTime = common.StringPtr(timeStr)
		cdnRequest.EndTime = common.StringPtr(timeStr)
		cdnRequest.Metric = common.StringPtr("request")
		cdnRequest.Domains = common.StringPtrs([]string{dimension.Domain})

		cdnResponse, err := cdnClient.DescribeCdnData(cdnRequest)
		if err != nil {
			if _, ok := err.(*errors.TencentCloudSDKError); ok {
				fmt.Printf("An API error has returned: %s", err)
				return
			}
			panic(err)
		}
		requestCount := cdnResponse.Response.Data[0].CdnData[0].SummarizedData.Value

		for _, metric := range e.tencentConfig.Metrics {
			wg.Add(1)

			go func(dimension config.CustomQueryDimension, metric string) {
				defer wg.Done()

				cdnRequest := cdn.NewDescribeCdnDataRequest()
				cdnRequest.StartTime = common.StringPtr(timeStr)
				cdnRequest.EndTime = common.StringPtr(timeStr)
				cdnRequest.Metric = common.StringPtr(metric)
				cdnRequest.Domains = common.StringPtrs([]string{dimension.Domain})

				cdnResponse, err := cdnClient.DescribeCdnData(cdnRequest)
				if err != nil {
					if _, ok := err.(*errors.TencentCloudSDKError); ok {
						fmt.Printf("An API error has returned: %s", err)
						return
					}
					panic(err)
				}

				fmt.Println(timeStr, *cdnResponse.Response.RequestId)

				if *cdnResponse.Response.Data[0].CdnData[0].SummarizedData.Value == 0 {
					fmt.Printf("[%s] %s summarized data value is nil, skip\n", dimension.Domain, metric)
					return
				}

				for _, data := range cdnResponse.Response.Data[0].CdnData {
					ch <- prometheus.MustNewConstMetric(
						e.cdnInstance,
						prometheus.GaugeValue,
						math.Round(*data.SummarizedData.Value/(*requestCount)*10000)/10000,
						[]string{
							dimension.Domain, // domain
							"tencent",        // provider
							*data.Metric,     // Status code
						}...,
					)
				}
			}(dimension, metric)
		}
	}

	wg.Wait()
}
