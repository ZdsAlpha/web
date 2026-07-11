# ---- Build stage ----
# Pinned to a patched Go release; bump as new patches ship.
FROM golang:1.26.5 AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a static, stripped binary. Generated files (view/*_templ.go,
# static/css/chroma.css) and assets are committed and embedded via //go:embed,
# so no codegen runs here and the runtime image needs only the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/web .

# ---- Runtime stage ----
# distroless static: ships CA certs + tzdata, runs as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/web /web

EXPOSE 8080
ENV PORT=8080
USER nonroot:nonroot
ENTRYPOINT ["/web"]
