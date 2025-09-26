.PHONY:
	local
	run
	test
# The version that will be used in docker tags
VERSION ?= $(shell git rev-parse --short HEAD)
ENTRY_POINT=./cmd

image: # Build docker image
	docker build -t kube-hpa-scale-to-zero:$(VERSION) .

local: # Run go application locally
	go run ${ENTRY_POINT} \
	--kube-config=${HOME}/.kube/config \
	--development-mode=true

test: # run tests
	go test -v  ./...

run: image # Run docker container in foreground
	docker run -p 9000:9000 kube-hpa-scale-to-zero:$(VERSION)