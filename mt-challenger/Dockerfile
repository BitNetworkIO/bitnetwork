FROM golang:1.19.0-alpine3.15 as builder

RUN apk add --no-cache make gcc musl-dev linux-headers git jq bash

COPY ./mt-challenger /go/mt-challenger
COPY ./datalayr /go/datalayr
COPY ./l2geth /go/l2geth
COPY ./mt-batcher /go/mt-batcher
COPY ./bss-core /go/bss-core
COPY ./fraud-proof /go/fraud-proof
COPY ./metrics  /go/metrics
COPY ./mt-challenger/docker.go.work /go/go.work

WORKDIR /go/mt-challenger
RUN make mt-challenger

FROM alpine:3.15

RUN apk add --no-cache ca-certificates
COPY --from=builder /go/mt-challenger/mt-challenger /usr/local/bin/

WORKDIR /usr/local/bin

ENTRYPOINT ["mt-challenger"]
