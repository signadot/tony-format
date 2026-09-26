GO ?= go

# issuelib's Go codec (issuelib_gen.go) and Tony schema (schema_gen.tony) are
# generated from its //tony:schemagen markers by go-tony's tony-codegen, which
# this module has through its dependency on go-tony.
generate:
	$(GO) run github.com/signadot/tony-format/go-tony/cmd/tony-codegen -dir issuelib -schema-dir-flat issuelib

test:
	$(GO) test ./...

install:
	$(GO) install .

.PHONY: generate test install
