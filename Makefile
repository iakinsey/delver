default:
	go build .

test:
	go clean -testcache
	go test ./... | grep -v "no test files"
