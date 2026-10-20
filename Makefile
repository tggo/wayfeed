.PHONY: test build deploy dns
test:
	go vet ./... && go test ./...
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o deploy/build/wayfeed .
# Needs deploy/inventory.yml (see inventory.example.yml).
deploy: test build
	cd deploy && ansible-playbook -i inventory.yml deploy.yml
# Cloudflare: proxied A records for the apex and www, SSL Full (strict). Needs CF (API token) and SERVER_IP in the env.
dns:
	deploy/dns.sh
