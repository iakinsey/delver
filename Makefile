default:
	go build .
legacy:
	git archive -v -o delver-repo.tar.gz --format=tar.gz HEAD
	docker build . -t delver-build
	docker run -d delver-build sleep 10
	docker cp `docker ps | grep delver-build | tr -s ' ' | cut -d " " -f 1`:/build/delver .
	docker kill `docker ps | grep delver-build | tr -s ' ' | cut -d " " -f 1`
test:
	go clean -testcache
	go test -timeout 30s ./... | grep -v "no test files" 
