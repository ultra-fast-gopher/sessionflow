# Goルータ・middlewareの対応調査

調査日: 2026-10-30。実装対象は、ISUCON公式Go実装に現れるルータと、Goの主要なHTTPルータ・フレームワーク・middlewareチェーンです。
HTTP認証、CORS、圧縮などの個々のmiddlewareを作り直すものではなく、そのアプリケーションのmiddleware APIにSessionflowを組み込むためのadapterを提供します。

## ISUCON公式リポジトリ

`github.com/isucon` の公開リポジトリから23リポジトリのdefault branchを確認しました。過去の大会開催時点のタグを再現した一覧ではありません。
特にISUCON9予選は現在のdefault branchがchi v5です。

| 公式Go実装 | 確認したルータ |
| --- | --- |
| [ISUCON3 予選・本選](https://github.com/isucon/isucon3) | Gorilla/mux |
| [ISUCON4 予選・本選](https://github.com/isucon/isucon4) | Martini |
| [ISUCON5予選](https://github.com/isucon/isucon5-qualify)・[本選](https://github.com/isucon/isucon5-final) | Gorilla/mux |
| [ISUCON6予選](https://github.com/isucon/isucon6-qualify) | Gorilla/mux |
| [ISUCON6本選](https://github.com/isucon/isucon6-final) | Goji (`goji.io/pat`) |
| [ISUCON7予選](https://github.com/isucon/isucon7-qualify/blob/master/webapp/go/src/isubata/app.go) | Echo（v4以前のimport path） |
| [ISUCON7本選](https://github.com/isucon/isucon7-final) | Gorilla/mux |
| [ISUCON8予選](https://github.com/isucon/isucon8-qualify/blob/master/webapp/go/src/torb/app.go) | Echo（v4以前のimport path） |
| [ISUCON8本選](https://github.com/isucon/isucon8-final/blob/master/webapp/go/src/isucon8/isucoin/webapp/main.go) | httprouter |
| [ISUCON9予選](https://github.com/isucon/isucon9-qualify/blob/master/webapp/go/go.mod) | chi v5 |
| [ISUCON9本選](https://github.com/isucon/isucon9-final/blob/master/webapp/go/main.go) | Goji (`goji.io/pat`) |
| [ISUCON10予選](https://github.com/isucon/isucon10-qualify/blob/master/webapp/go/go.mod) | Echo（v4以前のimport path） |
| [ISUCON10本選](https://github.com/isucon/isucon10-final/blob/master/webapp/golang/go.mod) | Echo v4 |
| [ISUCON11予選](https://github.com/isucon/isucon11-qualify/blob/main/webapp/go/go.mod)・[本選](https://github.com/isucon/isucon11-final/blob/main/webapp/go/go.mod) | Echo v4 |
| [ISUCON12予選](https://github.com/isucon/isucon12-qualify/blob/main/webapp/go/go.mod)・[本選](https://github.com/isucon/isucon12-final/blob/main/webapp/go/go.mod) | Echo v4 |
| [ISUCON13](https://github.com/isucon/isucon13/blob/main/webapp/go/go.mod) | Echo v4 |
| [ISUCON14](https://github.com/isucon/isucon14/blob/main/webapp/go/main.go) | chi v5 |
| [ISUCON11事前講習](https://github.com/isucon/isucon11-prior) | Gorilla/mux |

ISUCON2にはGoソースがなく、ISUCON12事前講習のGoソースはbenchmarker側でした。
各ファイルのURL・確認したimportの機械可読な一覧は [isucon-inventory.json](isucon-inventory.json) にあります。
再調査はリポジトリのルートで `python3 scripts/research_isucon.py` を実行します。公開GitHub APIとrawソースだけを読み、アプリケーションの設定値やソース本文を一覧には保存しません。

## adapterの設計

- 標準`net/http`型のルータは、ResponseWriterの任意interfaceを維持したまま計測します。各ルータの登録済みテンプレートを取得し、URL実値には置き換えません。
- 独自contextを持つフレームワークは、context・ルート・レスポンスのnative APIを使います。認証済みuser ID等を取り出すcallbackを追加できます。
- エラーをmiddlewareの外側で描画するフレームワークは、error handlerも接続し、アプリ独自のerror handlerが出した最終status・bytesを記録します。
- Fiber/fasthttpの文字列は、次のリクエストでバッファが再利用されます。記録時に保存するmethod・routeをコピーし、contextそのものは保持しません。
- router依存は`integration/`配下の別モジュールに隔離します。本体・CLIのGo 1.23を維持し、利用するadapterの依存だけをアプリに導入します。

## 調査に使った主な公式API

| 対象 | 公式ソース・API |
| --- | --- |
| net/http | [Request.Pattern / ResponseWriter](https://pkg.go.dev/net/http) |
| Echo | [v3](https://github.com/labstack/echo/tree/v3.3.10)・[v4](https://github.com/labstack/echo/tree/v4.12.0)・[v5](https://github.com/labstack/echo/tree/v5.4.4) |
| chi | [route context / RoutePattern](https://github.com/go-chi/chi/blob/master/context.go) |
| Gin | [Context.FullPath / Writer](https://github.com/gin-gonic/gin/blob/master/context.go) |
| Gorilla/mux | [CurrentRoute / GetPathTemplate](https://github.com/gorilla/mux) |
| httprouter | [Router.Handle / Params](https://github.com/julienschmidt/httprouter/tree/v1.3.0) |
| Goji | [Mux / pat](https://github.com/goji/goji) |
| Fiber | [v2](https://github.com/gofiber/fiber/tree/v2.52.16)・[v3](https://github.com/gofiber/fiber/tree/v3.6.1) |
| fasthttp | [RequestCtx / RequestHandler](https://github.com/valyala/fasthttp) |
| BunRouter | [middleware / Request.Route](https://github.com/uptrace/bunrouter) |
| HttpTreeMux | [route context](https://github.com/dimfeld/httptreemux) |
| Iris | [middleware / GetCurrentRoute](https://github.com/kataras/iris) |
| Beego | [filter chain](https://github.com/beego/beego) |
| Revel | [filters / router](https://github.com/revel/revel) |
| Martini | [handlers / Route](https://github.com/go-martini/martini) |
| Macaron | [context / handlers](https://github.com/go-macaron/macaron) |
| Alice | [net/http middleware chain](https://github.com/justinas/alice) |
| Negroni | [net/http middleware chain](https://github.com/urfave/negroni) |
| Hertz | [native request context / middleware](https://github.com/cloudwego/hertz) |
| go-zero REST | [REST middleware / router](https://github.com/zeromicro/go-zero) |
| go-restful | [container filters / selected route](https://github.com/emicklei/go-restful) |

調査時点でEcho v4の最新v4.16.3とFiber v3のv3.6.1はGo 1.26を要求します。Go 1.23の既存アプリ向けのadapter基準バージョンと、最新版で確認した組合せは対応一覧で区別します。
