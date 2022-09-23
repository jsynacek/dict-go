FROM golang:1.19.1-bullseye as build
COPY dict.go go.mod /code/
RUN cd /code && go build

# Certs are needed for https.
FROM alpine:3.16.2 as certs
RUN apk update && apk add ca-certificates

FROM busybox:1.34.1-glibc
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=build /code/dict-go /dict-go/
COPY static /dict-go/static/
COPY templates /dict-go/templates/
RUN adduser -h /home/dict -D dict \
    && chown -R dict:dict /dict-go
USER dict
WORKDIR /dict-go
CMD /dict-go/dict-go
