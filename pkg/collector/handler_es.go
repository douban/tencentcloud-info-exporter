package collector

import (
	"fmt"
	"github.com/dddfiish/tencentcloud-info-exporter/pkg/config"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/regions"
	es "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/es/v20180416"
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
			prometheus.BuildFQName(config.NameSpace, "es", "instance"),
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
