ARG GO_VERSION=1.27
ARG ALPINE_VERSION=3.23
FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN mkdir -p /out && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gittan .

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates git
COPY --from=build /out/gittan /usr/local/bin/gittan

WORKDIR /repo
ENTRYPOINT ["gittan"]
CMD ["--auto"]