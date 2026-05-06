FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /fxevm ./cmd/fxevm

FROM busybox

COPY --from=build /fxevm /usr/local/bin/fxevm
EXPOSE 8545
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=30s \
    CMD ["fxevm", "healthcheck"]
ENTRYPOINT ["fxevm"]
