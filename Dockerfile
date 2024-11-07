FROM golang:alpine as builder-base
LABEL builder=true multistage_tag="witb"
RUN apk add --no-cache upx ca-certificates tzdata gcc g++ 

FROM builder-base as builder-modules
LABEL builder=true multistage_tag="witb"
ARG TARGETARCH
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
RUN go mod verify

FROM builder-modules as builder
LABEL builder=true multistage_tag="witb"
ARG TARGETARCH
WORKDIR /build
ADD ./templates ./templates
COPY *.go .
RUN ls -l /build
RUN go env -w CGO_ENABLED=1
#CGO_ENALBED=1 GOOS=linux GOARCH=${TARGETARCH}  -ldflags '-s -w -extldflags="-static"'
RUN GOARCH=${TARGETARCH} go build -o witb
RUN upx --best --lzma witb

FROM alpine:3.17
WORKDIR /app

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /build/templates /app/templates
COPY --from=builder /build/witb /app/
CMD ["/app/witb"]