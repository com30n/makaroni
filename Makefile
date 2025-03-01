# Makefile
DOCKER_IMAGE = ghcr.io/com30n/makaroni:latest
HELM_CHART_DIR = helm/makaroni
HELM_OUTPUT_DIR = helm-dist
HELM_REPO ?= myhelmrepo   # Specify the Helm chart repository name

.PHONY: all build dev docker-build docker-push helm-build helm-push

all: build docker-build helm-build

# Build the application
build:
	go build -o bin/makaroni cmd/makaroni/main.go

# Run the application with air for development
dev:
	air

# Build the Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE) .

# Push the Docker image to the registry
docker-push:
	docker push $(DOCKER_IMAGE)

# Package the Helm chart
helm-build:
	mkdir -p $(HELM_OUTPUT_DIR)
	helm package $(HELM_CHART_DIR) -d $(HELM_OUTPUT_DIR)

# Push the Helm chart (requires helm-push plugin)
helm-push:
	helm push $(HELM_OUTPUT_DIR)/* $(HELM_REPO)