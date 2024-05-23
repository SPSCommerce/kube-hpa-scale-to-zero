FROM golang:1.22.0
COPY ./src /sources
WORKDIR /sources
RUN go build -ldflags "-s" -o run ./cmd


FROM golang:1.22.0
COPY --from=0 /sources/run /app/run
WORKDIR /app
ENTRYPOINT ["/app/run"]
CMD ["--hpa-selector", "spscommerce.com/scaleToZero=true", "--port", "8080"]
