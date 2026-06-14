# ---- Build stage ----
FROM golang:1.26 AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Generate templ components + syntax-highlighting CSS, then build a static,
# stripped binary. Assets are embedded via //go:embed, so the runtime image
# needs nothing but the binary.
RUN go run github.com/a-h/templ/cmd/templ generate \
 && go run ./tools/genchroma \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/web .

# ---- Runtime stage ----
# distroless static: ships CA certs + tzdata, runs as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/web /web

EXPOSE 8080
ENV PORT=8080
USER nonroot:nonroot
ENTRYPOINT ["/web"]
