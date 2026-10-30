module github.com/ultra-fast-gopher/sessionflow/integration/chi

go 1.23.0

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/ultra-fast-gopher/sessionflow v0.0.0
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace github.com/ultra-fast-gopher/sessionflow => ../..
