# ---- Build stage ----
FROM golang:1.26 AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a static, stripped binary. The generated files (view/*_templ.go and
# static/css/chroma.css) are committed to the repo, so no codegen runs here —
# regenerate them with `templ generate` + `go run ./tools/genchroma` before
# committing template/highlighting changes. Assets are embedded via //go:embed,
# so the runtime image needs nothing but the binary.
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
