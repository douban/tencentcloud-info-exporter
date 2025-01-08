FROM golang:alpine as build-env

RUN apk add git

# Copy source + vendor
COPY . /go/src/github.com/leoquote/tencentcloud-info-exporter
WORKDIR /go/src/github.com/leoquote/tencentcloud-info-exporter

# Build
ENV GOPATH=/go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -v -a -ldflags "-s -w" -o /go/bin/tencentcloud-info-exporter .

FROM library/alpine:3.15.0
COPY --from=build-env /go/bin/tencentcloud-info-exporter /usr/bin/tencentcloud-info-exporter
ENTRYPOINT ["tencentcloud-info-exporter"]
