FROM alpine as certs
RUN apk --update add ca-certificates

FROM scratch
ARG GOARCH
LABEL maintainer="squat <lserven@gmail.com>"
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY bin/linux/$GOARCH/fluxcdbot /
ENTRYPOINT ["/fluxcdbot"]
