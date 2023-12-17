FROM golang:1.17 as base

FROM base as packages
COPY go.mod go.sum /src/
WORKDIR /src
RUN CGO_ENABLED=0 go mod download

FROM packages as builder
COPY . /src
WORKDIR /src
RUN CGO_ENABLED=0 make build

FROM scratch AS exporter
COPY --from=builder /src/metrics-server-prometheus-exporter /
ENV HOME=/
ENTRYPOINT ["/metrics-server-prometheus-exporter"]