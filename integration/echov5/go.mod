module github.com/ultra-fast-gopher/sessionflow/integration/echov5

go 1.25.0

require (
	github.com/labstack/echo/v5 v5.4.4
	github.com/ultra-fast-gopher/sessionflow v0.0.0
)

require github.com/felixge/httpsnoop v1.0.4

replace github.com/ultra-fast-gopher/sessionflow => ../..
