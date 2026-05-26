FROM docker.io/library/golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /fxevm ./cmd/fxevm

FROM docker.io/library/busybox

COPY --from=build /fxevm /usr/local/bin/fxevm
RUN mkdir /data && chmod 777 /data
EXPOSE 8545
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=30s \
    CMD ["fxevm", "healthcheck"]
ENTRYPOINT ["fxevm"]
