.PHONY: build docker-build down up

all: build docker-build down up

build:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" ../../

docker-build:
	docker build -t ncode/bedel:dev .

up:
	echo > redis/redis0001.acl
	echo > redis/redis0002.acl
	echo > redis/redis0003.acl
	docker compose up

down:
	docker compose down
