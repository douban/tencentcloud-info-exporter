package collector

import (
	"context"
	"errors"
	"fmt"
	"github.com/dddfiish/tencentcloud-info-exporter/pkg/config"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	cdn "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdn/v20180606"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	terrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/regions"
	"golang.org/x/time/rate"
	"math"
	"sync"
	"time"
)

type CdnExporter struct {
	logger        log.Logger
	credential    common.CredentialIface
	tencentConfig config.TencentConfig

	cdnInstance *prometheus.Desc
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func NewCdnExporter(logger log.Logger, credential common.CredentialIface, tencentConfig config.TencentConfig) *CdnExporter {
	return &CdnExporter{
		logger:        logger,
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
		return
	}

	startTime := time.Now()
	timeStr := startTime.
		Add(-time.Duration(e.tencentConfig.CDN.DelaySeconds) * time.Second).
		In(time.FixedZone("Asia/Shanghai", 8*3600)).
		Format("2006-01-02 15:04:05")

	var wg sync.WaitGroup
	limiter := rate.NewLimiter(rate.Limit(e.tencentConfig.RateLimit), 1)

	for _, dimension := range e.tencentConfig.CDN.CustomQueryDimension {
		cdnRequest := cdn.NewDescribeCdnDataRequest()
		cdnRequest.StartTime = common.StringPtr(timeStr)
		cdnRequest.EndTime = common.StringPtr(timeStr)
		cdnRequest.Metric = common.StringPtr("request")
		cdnRequest.Domains = common.StringPtrs([]string{dimension.Domain})

		if err := limiter.Wait(context.Background()); err != nil {
			_ = level.Error(e.logger).Log("msg", "Rate limiter error", "err", err)
			continue
		}

		cdnResponse, err := cdnClient.DescribeCdnData(cdnRequest)
		if err != nil {
			var sdkErr *terrors.TencentCloudSDKError
			if errors.As(err, &sdkErr) {
				fmt.Printf("An API error has returned: %s\n", err)
				continue
			}
			_ = level.Error(e.logger).Log("msg", "CDN data request failed", "err", err)
			continue
		}

		requestCount := cdnResponse.Response.Data[0].CdnData[0].SummarizedData.Value

		for _, metric := range e.tencentConfig.CDN.Metrics {
			wg.Add(1)
			go func(dimension config.CustomQueryDimension, metric string) {
				defer wg.Done()

				cdnRequest := cdn.NewDescribeCdnDataRequest()
				cdnRequest.StartTime = common.StringPtr(timeStr)
				cdnRequest.EndTime = common.StringPtr(timeStr)
				cdnRequest.Metric = common.StringPtr(metric)
				cdnRequest.Domains = common.StringPtrs([]string{dimension.Domain})

				if err := limiter.Wait(context.Background()); err != nil {
					_ = level.Error(e.logger).Log("msg", "Rate limiter error", "err", err)
					return
				}

				cdnResponse, err := cdnClient.DescribeCdnData(cdnRequest)
				if err != nil {
					var sdkErr *terrors.TencentCloudSDKError
					if errors.As(err, &sdkErr) {
						fmt.Printf("An API error has returned: %s\n", sdkErr)
						return
					}
					_ = level.Error(e.logger).Log("msg", "CDN data request failed", "err", err)
					return
				}

				if *cdnResponse.Response.Data[0].CdnData[0].SummarizedData.Value == 0 {
					return
				}

				for _, data := range cdnResponse.Response.Data[0].CdnData {
					if !contains(e.tencentConfig.CDN.OnlyIncludeMetrics, *data.Metric) {
						continue
					}
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

	endTimeStr := time.Now().
		Add(-time.Duration(e.tencentConfig.CDN.DelaySeconds) * time.Second).
		In(time.FixedZone("Asia/Shanghai", 8*3600)).
		Format("2006-01-02 15:04:05")

	_ = level.Info(e.logger).Log("msg", "finish collect cdn data", "endTime", endTimeStr, "consumeTime", time.Since(startTime))
}
