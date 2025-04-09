all: build
.PHONY: all

.PHONY: build
build:
	go build -o kubectl-interact cmd/main.go