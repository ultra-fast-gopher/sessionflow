module github.com/ultra-fast-gopher/sessionflow/integration/httptreemux

go 1.23.0

require (
	github.com/dimfeld/httptreemux/v5 v5.5.0
	github.com/ultra-fast-gopher/sessionflow v0.0.0
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace github.com/ultra-fast-gopher/sessionflow => ../..
