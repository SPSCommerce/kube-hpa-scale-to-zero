FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
COPY . /sources
WORKDIR /sources

ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags "-s" -o run ./cmd

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /sources/run /app/run
WORKDIR /app
ENTRYPOINT ["/app/run"]
CMD ["--hpa-selector", "spscommerce.com/scaleToZero=true", "--port", "8080"]