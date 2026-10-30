module github.com/ultra-fast-gopher/sessionflow/integration/martini

go 1.23.0

replace github.com/ultra-fast-gopher/sessionflow => ../..

require (
	github.com/go-martini/martini v0.0.0-20170121215854-22fa46961aab
	github.com/ultra-fast-gopher/sessionflow v0.0.0-00010101000000-000000000000
)

require (
	github.com/codegangsta/inject v0.0.0-20150114235600-33e0aa1cb7c0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
)
