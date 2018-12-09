FROM quay.io/deis/lightweight-docker-go:v0.5.0
ARG BASE_PACKAGE_NAME
ARG LDFLAGS
ENV CGO_ENABLED=0
WORKDIR /go/src/$BASE_PACKAGE_NAME/
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY vendor/ vendor/
RUN go build -o bin/osiris -ldflags "$LDFLAGS" ./cmd

FROM alpine:3.8
ARG BASE_PACKAGE_NAME
RUN addgroup -S -g 1000 osiris \
  && adduser -S -u 1000 -G osiris -s /sbin/nologin -H osiris \
  && apk add --update iptables
COPY bin/ /osiris/bin/
COPY --from=0 /go/src/$BASE_PACKAGE_NAME/bin/ /osiris/bin/
