package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ultra-fast-gopher/sessionflow/analysis"
)

func TestReportEscapesUntrustedRoutes(t *testing.T) {
	r := &analysis.Report{Nodes: []analysis.Node{{ID: "GET </script><script>alert(1)</script>"}}}
	var out bytes.Buffer
	if err := Write(&out, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "GET </script>") || strings.Contains(out.String(), "__SESSIONFLOW_DATA__") {
		t.Fatal("unsafe or missing JSON embedding")
	}
	if !strings.Contains(out.String(), `\u003c/script\u003e`) {
		t.Fatal("missing escaped data")
	}
}
