FROM golang:alpine AS builder

RUN apk add --no-cache sqlite-libs sqlite-dev
RUN apk add --no-cache build-base git

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . ./

RUN CGO_ENABLED=1 go build -o /kosyncsrv

FROM alpine

VOLUME /data
WORKDIR /data

COPY --from=builder /kosyncsrv /

ENTRYPOINT "/kosyncsrv"
