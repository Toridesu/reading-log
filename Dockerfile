# ビルド用SDKは固定し、実行用イメージへは完成したバイナリとCA証明書だけを渡す。
FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY certs/ap-northeast-1-bundle.pem certs/SHA256SUMS ./certs/
RUN cd certs && sha256sum -c SHA256SUMS
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" -o /out/web ./cmd/web && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /src/certs/ap-northeast-1-bundle.pem /app/certs/ap-northeast-1-bundle.pem
WORKDIR /app
USER 65532:65532

# DB変更はWebの起動とは別のコマンド・イメージで実行する。
FROM runtime AS migrate
COPY --from=build /out/migrate /app/migrate
ENTRYPOINT ["/app/migrate"]
CMD ["status"]

FROM runtime AS web
COPY --from=build /out/web /app/web
COPY --from=build /out/healthcheck /app/healthcheck
ENV APP_ADDR=0.0.0.0:8000
EXPOSE 8000
ENTRYPOINT ["/app/web"]
