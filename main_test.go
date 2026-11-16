package main

import (
	"github.com/iakinsey/delver/application"
	"strings"
	"testing"
)

func TestInvalidConfiguration(t *testing.T) {
	if _, err := application.Decode(strings.NewReader(`{"workers":[{"name":"fetch","type":"http_fetcher","inbox":"missing"}]}`)); err == nil {
		t.Fatal("invalid configuration accepted")
	}
}
