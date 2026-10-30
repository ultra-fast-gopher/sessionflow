module github.com/ultra-fast-gopher/sessionflow/integration/gorillamux

go 1.23.0

require (
	github.com/gorilla/mux v1.8.1
	github.com/ultra-fast-gopher/sessionflow v0.0.0
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace github.com/ultra-fast-gopher/sessionflow => ../..
