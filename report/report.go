// Package report renders a self-contained offline HTML report.
package report

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"

	"github.com/ultra-fast-gopher/sessionflow/analysis"
)

//go:embed index.html
var page []byte

func Write(w io.Writer, r *analysis.Report) error {
	// encoding/json escapes <, >, &, and JS line separators, including </script>.
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = w.Write(bytes.Replace(page, []byte("__SESSIONFLOW_DATA__"), data, 1))
	return err
}
