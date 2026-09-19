.PHONY: build run test fmt docker

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/epay ./cmd/epay

run:
	go run ./cmd/epay -config config.yaml

test:
	go test ./...

fmt:
	gofmt -w .

docker:
	docker build -t epay-gateway .
