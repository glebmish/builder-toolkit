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

spec: $(SPEC_JSON)

$(SPEC_JSON): $(SPEC_YAML)
	@command -v yq >/dev/null 2>&1 || { echo "install yq: brew install yq / snap install yq"; exit 1; }
	yq -o=json '.' $(SPEC_YAML) > $(SPEC_JSON)

test: spec
	go test ./...
	go vet ./...

build: spec
	go build -o $(NAME) .

install: build
	go install .
	@bin=$$(go env GOBIN); [ -z "$$bin" ] && bin=$$(go env GOPATH)/bin; \
		cp $$bin/$(MOD) $$bin/$(NAME)
