package collector

import (
	"context"
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	cdn "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdn/v20180606"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/regions"
	"golang.org/x/time/rate"
	"math"
	"sync"
	"time"
)

const (
	NameSpace = "tc_info"
)

type CdnExporter struct {
	logger       log.Logger
	rateLimit    int
	delaySeconds int
	domains      []string
	credential   common.CredentialIface

	cdnInstance *prometheus.Desc
}

func NewCdnExporter(rateLimit int, delaySeconds int, domains []string, logger log.Logger, credential common.CredentialIface) *CdnExporter {
	return &CdnExporter{
		logger:       logger,
		rateLimit:    rateLimit,
		delaySeconds: delaySeconds,
		domains:      domains,
		credential:   credential,

		cdnInstance: prometheus.NewDesc(
			prometheus.BuildFQName(NameSpace, "cdn", "http_status_rate"),
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

	startTime := time.Now()
	timeStr := startTime.Add(-time.Duration(e.delaySeconds) * time.Second).
		In(time.FixedZone("Asia/Shanghai", 8*3600)).
		Format("2006-01-02 15:04:05")

	limiter := rate.NewLimiter(rate.Limit(e.rateLimit), 1)

	var wg sync.WaitGroup

	for _, dimension := range e.domains {
		wg.Add(1)
		go func(dimension string) {
			defer wg.Done()

			// First API call
			cdnRequest := cdn.NewDescribeCdnDataRequest()
			cdnRequest.StartTime = common.StringPtr(timeStr)
			cdnRequest.EndTime = common.StringPtr(timeStr)
			cdnRequest.Metric = common.StringPtr("request")
			cdnRequest.Domains = common.StringPtrs([]string{dimension})

			if err := limiter.Wait(context.Background()); err != nil {
				_ = level.Error(e.logger).Log("msg", "Rate limiter error", "err", err)
				return
			}

			cdnResponse, err := cdnClient.DescribeCdnData(cdnRequest)
			if _, ok := err.(*errors.TencentCloudSDKError); ok {
				_ = level.Error(e.logger).Log("msg", "API call error", "err", err)
				return
			}
			if err != nil {
				panic(err)
			}

			requestCount := cdnResponse.Response.Data[0].CdnData[0].SummarizedData.Value

			// Second API call
			cdnRequest = cdn.NewDescribeCdnDataRequest()
			cdnRequest.StartTime = common.StringPtr(timeStr)
			cdnRequest.EndTime = common.StringPtr(timeStr)
			cdnRequest.Metric = common.StringPtr("5xx")
			cdnRequest.Domains = common.StringPtrs([]string{dimension})

			if err := limiter.Wait(context.Background()); err != nil {
				_ = level.Error(e.logger).Log("msg", "Rate limiter error", "err", err)
				return
			}

			cdnResponse, err = cdnClient.DescribeCdnData(cdnRequest)
			if _, ok := err.(*errors.TencentCloudSDKError); ok {
				fmt.Printf("An API error has returned: %s", err)
				return
			}
			if err != nil {
				_ = level.Error(e.logger).Log("msg", "API call error", "err", err)
				return
			}

			if *cdnResponse.Response.Data[0].CdnData[0].SummarizedData.Value == 0 {
				return
			}

			for _, data := range cdnResponse.Response.Data[0].CdnData {
				if *data.Metric != "566" {
					continue
				}
				ch <- prometheus.MustNewConstMetric(
					e.cdnInstance,
					prometheus.GaugeValue,
					math.Round(*data.SummarizedData.Value/(*requestCount)*10000)/10000,
					[]string{
						dimension,    // domain
						"tencent",    // provider
						*data.Metric, // Status code
					}...,
				)
			}
		}(dimension)
	}

	wg.Wait()

	endTimeStr := time.Now().
		Add(-time.Duration(e.delaySeconds) * time.Second).
		In(time.FixedZone("Asia/Shanghai", 8*3600)).
		Format("2006-01-02 15:04:05")

	_ = level.Info(e.logger).Log("msg", "finish collect cdn data", "endTime", endTimeStr, "consumeTime", time.Since(startTime))
}
