FROM golang:1.27.4-alpine AS bau
WORKDIR /quelle
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=entwicklung
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /pagr .

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 pagr
ENV TZ=Europe/Berlin
USER pagr
COPY --from=bau /pagr /usr/local/bin/pagr
EXPOSE 8360
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8360/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/pagr"]
