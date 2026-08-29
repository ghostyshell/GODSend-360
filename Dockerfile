# GODsend 360 headless backend - see docs/headless-setup.md#docker
FROM golang:1.24-alpine AS build
WORKDIR /build
COPY src/server/go.mod src/server/go.sum ./
RUN go mod download
COPY src/server/ .
RUN CGO_ENABLED=0 go build -o /godsend .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates aria2
RUN adduser -D -u 1000 godsend && mkdir -p /data && chown godsend:godsend /data
COPY --from=build /godsend /usr/local/bin/godsend
ENV GODSEND_HOME=/data
EXPOSE 8080
USER godsend
ENTRYPOINT ["godsend"]
