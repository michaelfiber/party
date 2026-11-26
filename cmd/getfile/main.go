// getfile downloads one file from a cloud account using the credentials.json
// that the debug dump saved when that cloud was connected.
//
//	getfile debug/dropbox/<username>/credentials.json <file-id>
//
// The file lands next to the credentials file. The id is the one in the "id"
// column of that account's listing-*.tsv.
package main

import (
	"fmt"
	"os"

	"party"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <credentials.json> <file-id>\n", os.Args[0])
		os.Exit(2)
	}
	out, err := party.FetchDebugFile(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "getfile: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}
