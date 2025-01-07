# tencentcloud-info-exporter

## qcloud.yaml example

```yaml
rate_limit: 20

cdn:
  metrics:
    - 2xx
    - 3xx
    - 4xx
    - 5xx
  delay_seconds: 360
  custom_query_dimensions:
    - projectId: id
      domain: domain
```
