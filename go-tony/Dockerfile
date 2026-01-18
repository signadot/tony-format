ARG TARGETPLATFORM=linux/amd64
FROM --platform=$TARGETPLATFORM ghcr.io/oras-project/oras:latest AS oras

FROM --platform=$TARGETPLATFORM alpine:3.20

ARG GOOS=linux
ARG GOARCH=amd64

RUN apk add --no-cache \
    sed \
    gawk \
    grep

COPY ${GOOS}/${GOARCH}/o /usr/local/bin/o
COPY ${GOOS}/${GOARCH}/convert-image /usr/local/bin/convert-image
COPY --from=oras /bin/oras /usr/local/bin/oras

ENTRYPOINT ["/usr/local/bin/o"]
