module github.com/ultra-fast-gopher/sessionflow/integration/httpcompat

go 1.23.0

require (
	github.com/justinas/alice v1.2.0
	github.com/ultra-fast-gopher/sessionflow v0.0.0
	github.com/urfave/negroni v1.0.0
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace github.com/ultra-fast-gopher/sessionflow => ../..
