.PHONY: test vet build

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/legalscout
