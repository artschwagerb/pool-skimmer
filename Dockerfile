# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27.1

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS dependencies

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download


FROM dependencies AS test

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN go test ./...
RUN go vet ./...


FROM test AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/pool-skimmer ./cmd/pool-skimmer


FROM scratch AS binary

COPY --from=build /out/pool-skimmer /pool-skimmer


FROM scratch AS runtime

LABEL org.opencontainers.image.title="Pool Skimmer SCIM CLI" \
      org.opencontainers.image.description="Provider-neutral SCIM 2.0 user and group management CLI"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/pool-skimmer /pool-skimmer

USER 65532:65532

ENTRYPOINT ["/pool-skimmer"]
