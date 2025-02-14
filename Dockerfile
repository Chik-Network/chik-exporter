FROM golang:1 AS builder

COPY . /app
WORKDIR /app
RUN make build

FROM alpine:latest

COPY --from=builder /app/bin/chik-exporter /chik-exporter
CMD ["/chik-exporter", "serve"]
