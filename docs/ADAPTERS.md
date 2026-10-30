# Go adapterの対応一覧

adapterは`github.com/ultra-fast-gopher/sessionflow/integration/<名前>`という独立したGoモジュールです。
ルータの調査元は [FRAMEWORKS.md](FRAMEWORKS.md)、公式ISUCONの確認結果は [isucon-inventory.json](isucon-inventory.json) にあります。

## 接続方法

| 対象 | adapter | 接続API・登録済みテンプレート |
| --- | --- | --- |
| net/http / ServeMux | [本体](../recorder.go) | `rec.Wrap(mux)` / `Request.Pattern` |
| Echo v3 | [echo](../integration/echo) | `Install(e, rec)` / `c.Path()` |
| Echo v4 | [echov4](../integration/echov4) | `Install(e, rec)` / `c.Path()` |
| Echo v5 | [echov5](../integration/echov5) | `Install(e, rec)` / `c.Path()` |
| chi v4 | [chiv4](../integration/chiv4) | `router.Use(Middleware(rec))` / `RoutePattern()` |
| chi v5 | [chi](../integration/chi) | `router.Use(Middleware(rec))` / `RoutePattern()` |
| Gin | [gin](../integration/gin) | `engine.Use(Middleware(rec))` / `FullPath()` |
| Gorilla/mux | [gorillamux](../integration/gorillamux) | `Install(router, rec)` / `GetPathTemplate()`。`Middleware`単独も利用可 |
| httprouter | [httprouter](../integration/httprouter) | `Handle(rec, template, handler)` / 登録時のtemplate。native `Params`を保持 |
| Goji v2 | [goji](../integration/goji) | `mux.Use(Middleware(rec))` / `middleware.Pattern` |
| 旧Goji (`zenazn/goji`) | [gojiold](../integration/gojiold) | `Handler(rec, template, handler)` / 登録時のtemplate。native `web.C`を保持 |
| Fiber v2 | [fiberv2](../integration/fiberv2) | `app.Use(Middleware(rec))`と`ErrorHandler` / `c.Route().Path` |
| Fiber v3 | [fiberv3](../integration/fiberv3) | `app.Use(Middleware(rec))`と`ErrorHandler` / `c.Route().Path` |
| fasthttp | [fasthttp](../integration/fasthttp) | `Wrap(rec, handler, Options{Route: ...})` / native callback |
| Hertz | [hertz](../integration/hertz) | `engine.Use(Middleware(rec))` / `FullPath()` |
| BunRouter | [bunrouter](../integration/bunrouter) | `Middleware`または`MiddlewareWithErrorHandler` / `Request.Route()` |
| HttpTreeMux v5 | [httptreemux](../integration/httptreemux) | contextモードの`UseHandler(Middleware(rec))` / `ContextRoute` |
| Iris v12 | [iris](../integration/iris) | `app.UseRouter(Middleware(rec))` / `GetCurrentRoute().Path()` |
| Beego v2 | [beego](../integration/beego) | `InsertFilterChain("/*", FilterChain(rec))` / `RouterPattern` |
| Revel | [revel](../integration/revel) | `Filter(rec, routeResolver)`をfilter chainの先頭に置く / controller actionに対応する登録済みroute |
| Martini | [martini](../integration/martini) | routeごとのhandler列に`Middleware(rec)` / native `Route.Pattern()` |
| Macaron v1 | [macaron](../integration/macaron) | routeごとのhandler列に`Middleware(rec, template)` / 登録時のtemplate |
| go-zero REST | [gozero](../integration/gozero) | `Handler(rec, template, handler)` / 登録時のtemplate |
| go-restful v3 | [restful](../integration/restful) | `container.Filter(Filter(rec))` / `SelectedRoutePath()` |
| Alice | [互換テスト](../integration/httpcompat) | `alice.New(rec.Wrap).Then(handler)` |
| Negroni | [互換テスト](../integration/httpcompat) | `UseHandler(rec.Wrap(handler))` |

標準HTTP型のmiddlewareは、`Options.Session`で`req.Context()`内の認証情報を参照できます。
独自contextを持つadapterには`Config.Session`があります。Cookie名の既定値は本体の`CookieName`を使い、保存時には共通のSHA-256処理を通します。

## Goアプリから参照する

リリースtagはまだありません。アプリと`sessionflow/`のcheckoutが同じ親ディレクトリにある場合、Echo v4は次のように参照できます。

```sh
go mod edit -require=github.com/ultra-fast-gopher/sessionflow@v0.0.0
go mod edit -replace=github.com/ultra-fast-gopher/sessionflow=../sessionflow
go mod edit -require=github.com/ultra-fast-gopher/sessionflow/integration/echov4@v0.0.0
go mod edit -replace=github.com/ultra-fast-gopher/sessionflow/integration/echov4=../sessionflow/integration/echov4
go mod tidy
```

利用側の`go.mod`にも本体の`replace`が必要です。依存モジュール内の`replace`は利用側に引き継がれません。
別のルータの場合は、`echov4`を上の表のadapter名に置き換えてください。

```go
import (
    "github.com/labstack/echo/v4"
    flow "github.com/ultra-fast-gopher/sessionflow"
    sfecho "github.com/ultra-fast-gopher/sessionflow/integration/echov4"
)

rec, err := flow.New(flow.Options{CookieName: "session", MaxEvents: 100_000})
if err != nil { panic(err) }
e := echo.New()
// 独自のHTTPErrorHandlerがあればここで設定する
sfecho.Install(e, rec)
e.GET("/items/:id", showItem)
```

ルート登録の前にnative middlewareを設置し、認証・recovery・logger等を包む外側に置きます。error handlerはadapter接続より前に設定します。
同じリクエストに`rec.Wrap`とnative adapterを二重に設置すると二重計測になります。

## Echo・Fiberのエラー応答

Echo・Fiberは、handlerから返ったエラーをmiddlewareの外で描画することがあります。
そのため、middlewareに加えてerror handlerを接続し、独自のerror handlerが書いた最終status・bytesを記録します。

- Echo: `Install(e, rec)`が両方を接続します。手動で分ける場合は`e.Use(Middleware(rec))`と`InstallErrorHandler(e)`です。
- Fiber: アプリ作成時に`fiber.Config{ErrorHandler: adapter.ErrorHandler(existingErrorHandler)}`を渡し、`app.Use(adapter.Middleware(rec))`を設定します。既定のerror handlerを使う場合は`ErrorHandler(nil)`です。
- BunRouter: エラー描画も計測する場合、`MiddlewareWithErrorHandler(rec, renderer)`に描画callbackを渡します。callbackが返すerrorはそのまま呼び出し元に返します。計測無効時も描画callbackは実行されます。

これらのerror hookを省略した構成では、後で描画された応答まで計測できません。
計測対象のhandlerがpanicし、外側でrecoveryした場合は`panicked: true, status: 0`になります。recoveryを計測の内側に置いた場合は、recoveryが描画した応答を記録します。
panic中は拡張callbackを呼ばず、アプリの元のpanicを保持します。この場合はsessionが未特定になることがあります。

## native context・streaming

Fiber・fasthttp・Hertzでは、本体の`Options.Session/Route/Include`は`net/http`用のため使いません。adapterのnative `Config`または`Options` callbackを使います。
`Include`を指定すると、既定の`/debug/`除外を置き換えます。既定の除外も必要ならcallback内で併せて判定してください。
計測無効時はこれらのcallbackを呼びません。

Fiber・fasthttp等のbody streamを、計測のために読み取りません。送信量を取得できない場合は`bytes: 0, bytes_unknown: true`を返します。
context・リクエスト・レスポンスそのものを保存せず、再利用されるmethod・routeの文字列を記録時にコピーします。

## ルータAPIごとの境界

- `httprouter`・旧Goji・Macaron・go-zero等の登録時templateを渡すAPIでは、ルータに登録したものと同じtemplateを渡してください。既存のnativeパラメータは保持します。
- Gorilla/muxの`Middleware`だけでは、ルータ自身の404/405がmiddlewareを通りません。`Install`はこれらのhandlerも接続し、既存のfallbackを保ちます。
- Martini・Macaron・旧Goji・httprouter・go-zeroのrouteごとのwrapperは、未登録routeの404を観測しません。登録済みhandlerが返す404は観測します。
- HttpTreeMuxのhandler middlewareとgo-restfulのfilterは、ルータがdispatchしたhandlerの範囲を観測します。
- Revelはcontroller actionに対する登録済みrouteが一つなら自動解決します。複数のaliasがある場合はroute resolverを渡します。解決できない場合は`<unmatched>`です。Resultの描画後に計測を完了します。
- Alice・Negroni自体はルータではありません。内側が標準ServeMuxなら`Request.Pattern`を使い、それ以外はそのルータ用adapterまたは明示的な`Options.Route`を使います。

## テストの実行

```sh
make test                  # 本体 + 全adapter、race detector + go vet
make test-core             # 本体・CLIのみ
make test-integrations     # 全adapterのみ、最大2並列
python3 scripts/test_integrations.py integration/echov4 integration/chi
```

adapterのGoバージョンや依存関係は、そのディレクトリの`go.mod`に固定しています。新しいGo toolchainが必要なadapterは本体と別にビルドされます。
確認した組合せ・テスト結果は下の検証表に記録します。

## 検証バージョン

| adapter | 基準バージョン | adapterのgo.mod |
| --- | --- | --- |
| echo | v3.3.10 | Go 1.23 |
| echov4 | v4.12.0 | Go 1.23 |
| echov5 | v5.4.4 | Go 1.25 |
| chi | v5.2.5 | Go 1.23 |
| chiv4 | v4.1.2 | Go 1.23 |
| gin | v1.10.0 | Go 1.23 |
| gorillamux | v1.8.1 | Go 1.23 |
| httprouter | v1.3.0 | Go 1.23 |
| goji | v2.0.2 | Go 1.23 |
| gojiold | v0.9.0 | Go 1.23 |
| fiberv2 | v2.52.16 | Go 1.23 |
| fiberv3 | v3.6.1 | Go 1.26 |
| fasthttp | v1.57.0 | Go 1.23 |
| hertz | v0.10.6 | Go 1.23 |
| bunrouter | v1.0.23 | Go 1.23 |
| httptreemux | v5.5.0 | Go 1.23 |
| iris | v12.2.11 | Go 1.23 |
| beego | v2.3.10 | Go 1.24.2 |
| revel | v1.1.0 | Go 1.23 |
| martini | 2017-01-21のcommit `22fa46961aab` | Go 1.23 |
| macaron | v1.5.1 | Go 1.24 |
| gozero | v1.10.0 | Go 1.23 |
| restful | v3.14.0 | Go 1.23 |
| httpcompat | Alice v1.2.0 / Negroni v1.0.0 | Go 1.23 |

基準バージョンは、既存アプリとの互換性を優先した依存の選択です。利用側アプリがより新しいフレームワークを要求すると、そのバージョンがGoの依存解決で選ばれます。
追加で、基準モジュールを変更しない一時modfileを使い、次の組合せを確認しています。

| 新しいフレームワーク | 確認したGo toolchain |
| --- | --- |
| chi v5.5.0 | Go 1.24.4 |
| Echo v4.16.3 | Go 1.26.10 |
| Gin v1.12.0 | Go 1.25.3 |
| go-zero v1.10.3 | Go 1.24.4 |
| fasthttp v1.77.0 | Go 1.26.10 |

各基準adapterでは、フレームワーク自身のルータを通したリクエストでtemplate・実際のstatus・bytes・sessionのハッシュ化を確認します。route wrapper方式の404境界は上記のとおりです。
Revelは実際のRouter/RouterFilterとResult.Applyをテストしており、生成済みRevelアプリのserver起動を含むテストではありません。

Go 1.23を使う既存Echoアプリには`echo`または`echov4`を利用できます。Echo v4の最新v4.16.3とFiber v3のv3.6.1を選ぶ場合はGo 1.26が必要です。
