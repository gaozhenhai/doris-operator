FROM alpine:3.17

WORKDIR /
COPY bin/dorisoperator .

ENTRYPOINT ["/dorisoperator"]
