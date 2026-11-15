default: ui
	go build .

ui:
	npm --prefix web run build
	node scripts/package-ui.mjs
legacy:
	git archive -v -o delver-repo.tar.gz --format=tar.gz HEAD
	docker build . -t delver-build
	docker run -d delver-build sleep 10
	docker cp `docker ps | grep delver-build | tr -s ' ' | cut -d " " -f 1`:/build/delver .
	docker kill `docker ps | grep delver-build | tr -s ' ' | cut -d " " -f 1`
test:
	go clean -testcache
	go test -timeout 60s ./...

test-live-es:
	DELVER_ES_URL="$${DELVER_ES_URL:-http://localhost:9200}" go test -race -count=1 -timeout 90s ./gateway -run '^TestLiveElasticsearch$$'
