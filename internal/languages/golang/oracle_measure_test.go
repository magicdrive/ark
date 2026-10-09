package golang_test

import (
	"os"
	"testing"
)

// TestGoOracleMeasure compares Ark's Go resolutions with the go/types oracle
// over a repository. Measurement only:
//
//	ARK_GO_ORACLE_ROOT=/path/to/repo go test -run TestGoOracleMeasure -v ./internal/languages/golang/
func TestGoOracleMeasure(t *testing.T) {
	root := os.Getenv("ARK_GO_ORACLE_ROOT")
	if root == "" {
		t.Skip("set ARK_GO_ORACLE_ROOT to measure")
	}
	rep, err := compareWithOracle(root, 25)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + rep.String())
}
