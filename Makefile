default:
	go build .
legacy:
	rm -f delver
	docker build . -t delver-build
	docker run -d delver-build
	docker cp `docker ps | grep delver-build | tr -s ' ' | cut -d " " -f 1`:/build/delver .
	docker kill `docker ps | grep delver-build | tr -s ' ' | cut -d " " -f 1`
test:
	go clean -testcache
	go test -timeout 30s ./... | grep -v "no test files" 
