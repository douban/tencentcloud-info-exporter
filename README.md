# tencentcloud-info-exporter

## qcloud.yaml example

```yaml
rate_limit: 20
cdn:
  metrics:
    - 5xx
  only_include_metrics:
    - 566
  delay_seconds: 360
  custom_query_dimensions:
    - projectId: id
      domain: domain
```
