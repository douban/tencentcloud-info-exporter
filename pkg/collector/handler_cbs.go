package collector

import (
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	cbs "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"tencentcloud-info-exporter/pkg/config"
)

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
			prometheus.BuildFQName(config.NameSpace, "cbs", "instance"),
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
