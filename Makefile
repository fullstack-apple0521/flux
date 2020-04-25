VERSION?=$(shell grep 'VERSION' cmd/tk/main.go | awk '{ print $$4 }' | tr -d '"')

all: test build

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

test: tidy fmt vet
	go test ./... -coverprofile cover.out

build:
	CGO_ENABLED=0 go build -o ./bin/tk ./cmd/tk

install:
	go install cmd/tk
