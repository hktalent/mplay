.PHONY: build desktop run dist docker clean fmt vet

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 1.0.0)

build:
	VERSION=$(VERSION) ./scripts/build.sh

run: build
	./bin/mplay -open

desktop:
	./scripts/desktop.sh

dist:
	VERSION=$(VERSION) ./scripts/package.sh

docker:
	docker build -f packaging/Dockerfile -t mplay:$(VERSION) .

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './dist/*')

vet:
	go vet ./...

clean:
	rm -rf bin dist
