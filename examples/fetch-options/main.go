// Regenerate the UI's field catalogue from the backend validation schema.
package main

import (
	"encoding/json"
	"github.com/iakinsey/delver/flow"
	"os"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(flow.Options()); err != nil {
		panic(err)
	}
}
