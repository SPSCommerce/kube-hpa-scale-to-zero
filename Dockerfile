FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
COPY . /sources
WORKDIR /sources

ARG TARGETARCH

RUN GOOS=linux GOARCH=$TARGETARCH go build -ldflags "-s" -o run ./cmd

FROM --platform=$TARGETPLATFORM golang:1.25
COPY --from=builder /sources/run /app/run
WORKDIR /app
ENTRYPOINT ["/app/run"]
CMD ["--hpa-selector", "spscommerce.com/scaleToZero=true", "--port", "8080"]