# Build Aoa in a stock Go builder container
FROM golang:1.10-alpine as builder

RUN apk add --no-cache make gcc musl-dev linux-headers

ADD . /go-aurora
RUN cd /go-aurora && make aoa

# Pull Aoa into a second stage deploy alpine container
FROM alpine:latest

RUN apk add --no-cache ca-certificates
COPY --from=builder /go-aurora/build/bin/aoa /usr/local/bin/

EXPOSE 8545 8546 30303 30303/udp
ENTRYPOINT ["aoa"]