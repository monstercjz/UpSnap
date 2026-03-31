FROM alpine:3 AS downloader
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ENV BUILDX_ARCH="${TARGETOS:-linux}_${TARGETARCH:-amd64}${TARGETVARIANT}"
WORKDIR /app
COPY backend/dist/upsnap_${BUILDX_ARCH} upsnap
RUN chmod +x upsnap && \
    apk update && \
    apk add --no-cache libcap && \
    setcap 'cap_net_raw=+p' ./upsnap

FROM alpine:3
ARG UPSNAP_HTTP_LISTEN=0.0.0.0:8090
ENV UPSNAP_HTTP_LISTEN=${UPSNAP_HTTP_LISTEN}
RUN apk update && \
    apk add --no-cache tzdata ca-certificates nmap samba samba-common-tools openssh sshpass curl && \
    rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=downloader /app/upsnap upsnap
HEALTHCHECK --interval=10s \
    CMD curl -fs "http://${UPSNAP_HTTP_LISTEN}/api/health" || exit 1
CMD ["serve","--http","${UPSNAP_HTTP_LISTEN}"]
ENTRYPOINT ["/app/upsnap"]