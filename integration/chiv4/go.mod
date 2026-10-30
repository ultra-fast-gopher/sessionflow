module github.com/ultra-fast-gopher/sessionflow/integration/chiv4

go 1.23.0

require (
	github.com/go-chi/chi v4.1.2+incompatible
	github.com/ultra-fast-gopher/sessionflow v0.0.0
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace github.com/ultra-fast-gopher/sessionflow => ../..
