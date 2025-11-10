include .env
export $(shell sed 's/=.*//' .env)

gen:
	protoc --proto_path=. --twirp_out=. --go_out=. rpc/*.proto

run:
	go run ./cmd/server

test:
	go test ./...

up:
	docker compose up -d

down:
	docker compose down
