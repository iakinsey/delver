package frontier

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

const robotsFileName = "robots.txt"
const testHttpServerPort = 23876

var scenarios = map[string]bool{
	"/good":     true,
	"/alsogood": true,
	"/bad":      false,
	"/alsobad":  false,
}

func TestMemoryRobots(t *testing.T) {
	go startRobotsServer()
	time.Sleep(1 * time.Second)

	client := util.NewHTTPClient(util.HTTPClientParams{})
	memoryRobots := NewMemoryRobots(client)

	for uri, expectedState := range scenarios {
		u := fmt.Sprintf("http://localhost:%d%s", testHttpServerPort, uri)
		actualState, err := memoryRobots.IsAllowed(u)

		assert.NoError(t, err)
		assert.Equal(t, expectedState, actualState)
	}
}

func startRobotsServer() {
	addr := fmt.Sprintf(":%d", testHttpServerPort)

	http.HandleFunc("/robots.txt", RobotsHandler)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}

}

func RobotsHandler(w http.ResponseWriter, r *http.Request) {
	robots, err := os.Open(util.DataFilePath(robotsFileName))

	if err != nil {
		log.Fatalf(err.Error())
	}

	if _, err = io.Copy(w, robots); err != nil {
		log.Fatalf(err.Error())
	}

}
