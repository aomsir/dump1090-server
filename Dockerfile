# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -a \
    -ldflags="-s -w" \
    -tags netgo \
    -o bin/dump1090-server \
    ./cmd/dump1090-server

# Runtime stage
FROM scratch

COPY --from=builder /build/bin/dump1090-server /dump1090-server

EXPOSE 30001 30002 30003 30004 30005 8080 10001

USER 1000:1000

ENTRYPOINT ["/dump1090-server"]
CMD ["--net-only", "--net"]
