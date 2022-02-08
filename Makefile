default:
	go build .

test:
	go clean -testcache
	go test -timeout 30s ./... | grep -v "no test files" 
