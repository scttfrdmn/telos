# Telos host (telosd) — ARM64 container for the AgentCore contract.
#
# Build locally with `make docker-arm64` (which passes --platform linux/arm64).
# This image is build-only in M0; deploying it to AgentCore is a separate, gated
# step. The seed (bootstrap.acs) is embedded in the binary via go:embed, so the
# runtime image needs no data files.

# --- build stage ---------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src

# Module graph first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static ARM64 binary (CGO off so it runs on a scratch/distroless base).
ARG TARGETOS=linux
ARG TARGETARCH=arm64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/telosd ./cmd/telosd

# --- runtime stage -------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/telosd /telosd

EXPOSE 8080
ENV TELOS_ADDR=0.0.0.0:8080
USER nonroot:nonroot
ENTRYPOINT ["/telosd"]
