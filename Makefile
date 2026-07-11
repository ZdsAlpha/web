GO ?= go
export GOTOOLCHAIN := local

.PHONY: check dev fmt generate run test

run:
	$(GO) run .

dev:
	DEV=1 $(GO) run .

fmt:
	gofmt -w $$(find . -type f -name '*.go' -not -path './vendor/*')
	$(GO) tool templ fmt view

generate:
	$(GO) tool templ generate
	$(GO) run ./tools/genchroma
	$(GO) run ./tools/genog

test:
	$(GO) test ./...

check:
	@files="$$(find . -type f -name '*.go' -not -path './vendor/*')"; \
		unformatted="$$(gofmt -l $$files)"; \
		test -z "$$unformatted" || \
		(echo "Go files need formatting:"; echo "$$unformatted"; exit 1)
	$(GO) tool templ fmt -fail view
	$(GO) mod tidy -diff
	$(GO) test ./...
	$(GO) vet ./...
