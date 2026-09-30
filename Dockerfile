FROM golang:latest AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . ./

RUN CGO_ENABLED=0 go build -o /kosyncsrv

FROM scratch

VOLUME /data
WORKDIR /data

COPY --from=builder /kosyncsrv /

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 8080

ENTRYPOINT ["/kosyncsrv"]
