module github.com/ultra-fast-gopher/sessionflow/integration/goji

go 1.23.0

require (
	github.com/ultra-fast-gopher/sessionflow v0.0.0
	goji.io v2.0.2+incompatible
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace github.com/ultra-fast-gopher/sessionflow => ../..
