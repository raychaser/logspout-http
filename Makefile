NAME=logspout-http
VERSION=$(shell cat VERSION)

dev:
	@docker build -f Dockerfile.dev -t $(NAME):dev .
	@docker run --rm \
		-e DEBUG=true \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD):/go/src/github.com/raychaser/logspout-http \
		-p 8000:80 \
		-e ROUTE_URIS="$(ROUTE)" \
		-e LOGSPOUT=ignore \
		$(NAME):dev

build:
	mkdir -p build
	docker build -t $(NAME):$(VERSION) .
	docker save $(NAME):$(VERSION) | gzip -9 > build/$(NAME)_$(VERSION).tgz

.PHONY: release