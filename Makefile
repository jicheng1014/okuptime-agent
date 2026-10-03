BINARY := okuptime
CMD := ./cmd/okuptime
GOARCH ?= amd64

.PHONY: build build-linux test vet install

build:
	go build -trimpath -o dist/$(BINARY) $(CMD)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -o dist/$(BINARY)-linux-$(GOARCH) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

install:
	go install $(CMD)
