# Snippet: Makefile.
#
# Substitute NAME (binary alias) and MOD (module-dir name from go.mod).
# `go install .` produces $GOBIN/$(MOD); the install target then aliases
# it to $GOBIN/$(NAME) so users invoke `acme`, not `acme-cli`.

SPEC_YAML := openapi.yaml
SPEC_JSON := internal/cmd/openapi-spec.json
NAME      := acme
MOD       := acme-cli

.PHONY: spec test build install

# Without this, a yq failure (malformed YAML, OOM, Ctrl-C) leaves a
# zero-byte spec whose mtime is newer than the source — so every later
# `make build` considers it up to date and the bijection test passes
# against a spec that isn't there.
.DELETE_ON_ERROR:

spec: $(SPEC_JSON)

$(SPEC_JSON): $(SPEC_YAML)
	@command -v yq >/dev/null 2>&1 || { echo "install yq: brew install yq / snap install yq"; exit 1; }
	@mkdir -p $(dir $(SPEC_JSON))
	yq -o=json '.' $(SPEC_YAML) > $(SPEC_JSON)

test: spec
	go test ./...
	go vet ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

build: spec
	go build -o $(NAME) .

install: build
	go install .
	@bin=$$(go env GOBIN); \
		if [ -z "$$bin" ]; then bin=$$(go env GOPATH)/bin; fi; \
		cp "$$bin/$(MOD)" "$$bin/$(NAME)"
