BINARY := okuptime
CMD := ./cmd/okuptime
GOARCH ?= amd64
VERSION ?= dev
UPDATE_PUBLIC_KEY ?=
LDFLAGS := -X main.version=$(VERSION) -X main.updatePublicKey=$(UPDATE_PUBLIC_KEY)

.PHONY: build build-linux test vet install

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) $(CMD)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-$(GOARCH) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

install:
	go install -ldflags "$(LDFLAGS)" $(CMD)
