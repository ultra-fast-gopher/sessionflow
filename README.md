# Sessionflow

[![CI](https://github.com/ultra-fast-gopher/sessionflow/actions/workflows/ci.yml/badge.svg)](https://github.com/ultra-fast-gopher/sessionflow/actions/workflows/ci.yml)

ISUCON用の、Go middleware + HTTP収集 + 行動解析CLI + オフラインHTMLレポート。
sessionごとに「どのルートから、どのルートへリクエストしたか」を調べるための独立ツールです。

```text
アプリ / middleware → 上限付きメモリ
                          ↑ start / stop (HTTP)
                   sessionflow collect → run.json
                                            ├─ analyze → JSON / text → AI・jq
                                            └─ report  → HTML → グラフ / session時系列
```

## まず試す

本体・CLIはGo 1.23以降。個別adapterの必要バージョンは [対応一覧](docs/ADAPTERS.md) を参照してください。ソースから使う場合は、ローカルcheckoutをreplaceして参照できます。CLIはGitHub Releasesでbinaryを配布します。

```sh
# リポジトリのルートで実行
make demo
open artifacts/demo.html
./bin/sessionflow analyze -input artifacts/demo.json -view edges -format text
```

`demo` は実際にmiddlewareを通した合成トラフィックです。24 session / 108 requests / 84 transitions。
ブラウザ表示は外部ライブラリ・CDN・サーバへの接続なしで動きます。
グラフ・遷移表をクリックすると、該当するsessionの時系列に絞り込めます。
時系列は同じMETHOD＋routeの並びをjourneyとしてまとめ、件数順に50パターンずつ表示します。各パターンから表示する代表session数は1/3/5/10で選べます（既定3）。

## CLIのrelease binary

`vX.Y.Z`タグをpushするとGitHub Releaseを作成し、次のOS / CPU向けCLIアーカイブと`SHA256SUMS`を添付します。

- Linux: amd64, arm64
- macOS: amd64, arm64
- Windows: amd64, arm64

```sh
git tag -a v0.1.0 -m 'v0.1.0'
git push origin v0.1.0
```

Releaseページから利用環境に合うアーカイブと`SHA256SUMS`を同じディレクトリに取得し、対象アーカイブのchecksumを確認できます。

```sh
archive=sessionflow_0.1.0_linux_amd64.tar.gz
grep "$archive" SHA256SUMS | sha256sum -c -      # Linux
grep "$archive" SHA256SUMS | shasum -a 256 -c - # macOS
```

## Goアプリへの導入

Echo・chi・Gin・Fiber等には専用adapterがあります。[対応一覧と導入方法](docs/ADAPTERS.md)、[ISUCON公式実装の調査結果](docs/FRAMEWORKS.md) を用意しています。

導入先のgo.modからローカルcheckoutを参照します。パスは配置に合わせて変更してください。

```sh
go mod edit -require=github.com/ultra-fast-gopher/sessionflow@v0.0.0
go mod edit -replace=github.com/ultra-fast-gopher/sessionflow=../sessionflow
go mod tidy
```

net/http の例です。controlはアプリと別のprivate listenerに置きます。

```go
rec, err := sessionflow.New(sessionflow.Options{
    CookieName: "session", // 実際のCookie名に合わせる
    ExcludePaths: []string{"/assets", "/healthz"},
    MaxEvents: 100_000,
    SampleEvery: 1,
})
if err != nil { log.Fatal(err) }

mux := http.NewServeMux()
mux.HandleFunc("GET /items/{id}", showItem)

debugMux := http.NewServeMux()
debugMux.Handle("/debug/sessionflow/", http.StripPrefix(
    "/debug/sessionflow", rec.ControlHandler(os.Getenv("SESSIONFLOW_TOKEN")),
))
go func() { log.Fatal(http.ListenAndServe("127.0.0.1:6060", debugMux)) }()
log.Fatal(http.ListenAndServe(":8080", rec.Wrap(mux)))
```

- `Start` するまで計測無効です。`Stop` でメモリを切り離して返します。
- `net/http.ServeMux` では `Request.Pattern` を使います。`GET /items/{id}` のmethod部分は除去し、nodeは `GET /items/{id}` と表記します。
- テンプレートが取れないルートは `<unmatched>` に集約します。実URLやクエリ文字列への自動フォールバックはありません。
- `Include func(*http.Request) bool` で静的ファイル等を除外できます。既定では `/debug/` を除外します。独自のIncludeを指定した場合、この既定値を置き換えます。
- `ExcludePaths: []string{"/assets", "/healthz"}` は指定pathとその配下を全adapterで除外します。`/assets` は `/assets` と `/assets/...` に一致し、`/assets-old` には一致しません。`Include`とは独立して適用されます。
- `Route func(*http.Request) string` でルータのテンプレートを渡せます。raw URLではなく、パラメータを正規化した値を返してください。
- `Session func(*http.Request) string` で独自のsession IDや認証済みuser IDを渡せます。このcallbackはhandler終了後に実行されます。生値は保存せず、鍵なしSHA-256のhex digestに変換します。
- Cookie欠落時は遷移を作りません。IP・User-Agentでユーザを推測せず、ルートごとの集計にだけ含めます。
- 同じsession値は全プロセス・captureで同じ64文字のSHA-256キーになります。session値が変わると別sessionとして扱います。鍵の設定や環境変数は不要です。
- このdigestは秘匿用途ではなく、session値の安定した照合キーです。captureをまたいで同じsession値を照合できます。
- `Source` を省略するとランダムIDを生成します。指定する場合はプロセス間で一意にしてください。
- Cookie更新・ログイン時のsessionローテーションで値が変わると別sessionになります。更新前後を結合したい場合は `Session` callbackで安定したIDを使います。初回レスポンスのSet-Cookieは自動追跡しません。

### Echo

Echo v4では`integration/echov4`を使います。アプリ側のgo.modに、本体に加えてadapterのローカル参照を追加してください。

```sh
go mod edit -require=github.com/ultra-fast-gopher/sessionflow/integration/echov4@v0.0.0
go mod edit -replace=github.com/ultra-fast-gopher/sessionflow/integration/echov4=../sessionflow/integration/echov4
go mod tidy
```

```go
// import sfecho "github.com/ultra-fast-gopher/sessionflow/integration/echov4"
e := echo.New()
// 独自のe.HTTPErrorHandlerがあれば、Installより前に設定する
sfecho.Install(e, rec)
e.GET("/items/:id", showItem)
```

`Install`はnative middlewareとerror handlerを接続します。middlewareを外側に置くため、`Install`は他の`e.Use`より前に呼びます。
登録済みルートを`c.Path()`から取り、アプリのerror handlerが描画したstatus・bytesを記録します。
認証済みIDを利用する場合は、native callbackを渡せます。

```go
sfecho.Install(e, rec, sfecho.Config{
    Session: func(c echo.Context) string {
        id, _ := c.Get("user_id").(string)
        return id // 空文字ならユーザ未特定。実際の認証処理のcontextに合わせる
    },
})
```

この二つは代替の導入例です。同じrecorderを二重にInstallしないでください。
Echo v3は`integration/echo`、v5は`integration/echov5`を使います。

### Chi

Chiでは、route contextが用意された後にmiddlewareを入れます。`integration/chi`を同じ方法でgo.modから参照します。

```go
// import sfchi "github.com/ultra-fast-gopher/sessionflow/integration/chi"
r := chi.NewRouter()
r.Use(sfchi.Middleware(rec))
r.Get("/items/{id}", showItem)
```

この例では `http.ListenAndServe(":8080", r)` とします。さらに外側を `rec.Wrap(r)` で包むと二重計測になります。
専用adapterと`rec.Wrap`を重ねず、いずれか一方を使います。ルータが公開しない情報は、登録時のテンプレートやnative callbackで明示できます。

## 収集

```sh
# 単一サーバ。pproteinと同じベンチ実行時間帯に立ち上げる
./bin/sessionflow collect \
  -target http://127.0.0.1:6060/debug/sessionflow \
  -duration 60s -id bench-001 -out artifacts/bench-001.json

# 複数サーバは各targetを並列start / stopし、一つのbundleに保存
./bin/sessionflow collect \
  -target http://app1:6060/debug/sessionflow \
  -target http://app2:6060/debug/sessionflow \
  -duration 60s -id bench-002 -out artifacts/bench-002.json
```

遠隔のprivate listenerへはSSH転送などを使ってください。`SESSIONFLOW_TOKEN` を設定するとBearer認証します。
複数サーバのsessionは同じSHA-256キーで結合されます。時刻同期が必要です。各サーバのstart / stop時刻はファイルに保存されます。HTTP制御なので開始・終了は完全には同時になりません。

`Ctrl-C` / `SIGTERM` でもstopして保存します。一部のstartが失敗した場合、成功したtargetをstopして部分データとエラーを保存し、終了コード1を返します。
stop自体が通信エラーになった場合も、成功したtargetのデータは保存します。エラーに出たcapture IDを使って状態を確認してください。
直近のstop結果は次のstartまで保持します。応答途中で通信が切れた場合、同じcapture IDでstopを再実行して取得できます。アプリプロセスの再起動をまたぐ永続化はありません。

ベンチの前後に明示的に制御する場合:

```sh
./bin/sessionflow start -target http://127.0.0.1:6060/debug/sessionflow -id bench-003
# ここでベンチを実行
./bin/sessionflow stop -target http://127.0.0.1:6060/debug/sessionflow -id bench-003 -out artifacts/bench-003.json
./bin/sessionflow status -target http://127.0.0.1:6060/debug/sessionflow
```

HTTP APIは `POST /start`・`POST /stop`（body: `{"capture_id":"bench-003"}`）と `GET /status`。
同じIDのstartは有効な計測中に限り冪等です。別IDとの同時計測は409になります。
停止は**その時点で記録が完了したリクエスト**を返します。まだ実行中のものは `in_flight` として数え、次の計測には混ぜません。

## AI・CLIから解析

```sh
# 既定はsummaryのJSON。診断メッセージはstderr
./bin/sessionflow analyze -input artifacts/bench-001.json

# 呼び出し回数の多いルート、遷移、頻出シーケンス
./bin/sessionflow analyze -input artifacts/bench-001.json -view nodes -top 20
./bin/sessionflow analyze -input artifacts/bench-001.json -view edges -top 20
./bin/sessionflow analyze -input artifacts/bench-001.json -view paths -top 20

# 特定ルートの次に何をしているか
./bin/sessionflow analyze -input artifacts/bench-001.json \
  -view edges -from 'GET /items/{id}'

# sessionを列挙して、特定sessionの全リクエストを取得
./bin/sessionflow analyze -input artifacts/bench-001.json -view sessions -top 10
./bin/sessionflow analyze -input artifacts/bench-001.json -view all -session SESSION_HASH

# 別々にstopした同一capture IDの複数サーバも結合可能
./bin/sessionflow analyze -input app1.json -input app2.json -view all

./bin/sessionflow report -input artifacts/bench-001.json -out artifacts/bench-001.html
./bin/sessionflow serve -input artifacts/bench-001.json -addr 127.0.0.1:9070
```

`-view all` は `summary`, `nodes`, `edges`, `paths`, `sessions` を含みます。
`-session` はSHA-256 hashまたは完全なsession ID（capture ID + hash scheme ID + hash）に一致させます。
異なるcapture IDや異なるsession hash IDのsessionは結合しません。同じcapture ID + sourceの二重読み込みはエラーにします。
JSONの解析結果の並びは回数降順・同率時は名前順で安定しています。sessionはID順です。

遷移の `count` / `sessions` / `probability` に加え、遷移先リクエストの `total_ms` / `p95_ms` / `errors`、前の実行区間からの `mean_gap_ms` を返します。
ルートの `errors` はHTTP 4xx・5xxとpanic。panicが外側で処理される場合、記録するstatusは0です。
Fiber/fasthttp等でstreaming本文の送信量を取得できない場合、`bytes_unknown: true`とし、本文を消費せず残します。ルート集計の`bytes_unknown`はそのリクエスト数です。
`-top` は配列の表示件数を制限します。`-from` / `-to` は辺の完全一致フィルタです。いずれもsummaryと確率の分母は変更しません。
`-session` を指定した場合、events / nodes / edgesは絞り込み後、seen / dropped等の収集カウンタはcapture全体です。

## 「遷移」の定義と限界

これは**サーバが観測したsession内のリクエスト列**です。HTMLページの画面遷移を識別するbrowser SDKではありません。
API・ポーリング・静的ファイル等も、除外しなければ列に含まれます。Refererによる親子関係・因果関係は推測しません。

1. 同一capture ID・同一hash ID・同一sessionをまとめ、**開始時刻順**に並べます。完了順で並べ替えません。
2. 隣接するリクエストを辺として集計します。ただし先行するリクエストがまだ実行中なら `overlap` として除外します。タイムラインには残します。
3. `-max-gap`（既定30分）を超える空白で列を切ります。
4. シーケンスは上の切断位置で区切った観測列です。同じsessionの再訪も回数に含みます。

並列区間終了後の辺は、開始順で直前にあったリクエストから作ります。gapはそれまでの全リクエストの終了時刻の最大値から測ります。
同時開始・時計ずれによる見かけのoverlapも除外対象です。複数サーバでの厳密な因果順序は保証できません。
欠損があると途中のリクエストが抜けた辺もできるため、警告付きの観測値として扱ってください。

## 負荷・メモリ

- リクエストごとのファイル・ネットワークI/O、goroutine起動、JSON化はありません。
- 固定容量のsliceに短いmutex区間で追記します。mutex待ちは発生し得ます。ロックフリー・無待機という設計ではありません。stop後も再取得のため直近の結果を保持し、次のstartで解放します。
- 上限到達時は新しい記録を落として `dropped` を数えます。記録バッファの追加拡張はしません。
- `MaxEvents: 100_000` のイベント本体は64bit環境で約9.6MB、これにsessionのhash文字列等が加わります。JSON出力・収集CLI・解析・HTMLは別途メモリを使います。
- `SampleEvery: 10` はおよそ1/10の**session全体**を記録します。観測した値を10倍に外挿しません。session不明のリクエストはsamplingせず記録します。
- samplingは応答時間を下げる仕組みではありません。ハッシュ・HTTP計測自体は実行するため、主に記録量と解析量を抑えます。
- collectorのHTTPレスポンス上限はtargetごとに128MiBです。大きなルート名やMaxEventsを使う場合は注意してください。

手元のmicrobenchmark（Apple M1 Max / darwin arm64 / Go 1.24.4、2026-10-30）:

| 条件 | ns/request | B/request | allocs/request |
| --- | ---: | ---: | ---: |
| handlerのみ | 3.1 | 0 | 0 |
| middleware設置・計測無効 | 7.4 | 0 | 0 |
| 記録有効 | 785 | 616 | 17 |

handlerは204を返すだけで、ルート・Cookieを固定した逐次呼び出しです。実ネットワーク、並列時のmutex競合、ISUCONのスコアへの影響はこの値に含みません。
`make bench` で再計測できます。アプリへの導入後は、無効時・有効時で実際のベンチを比較してください。

Echo v4の専用adapterでも、Cookie付きのリクエストで比較しました（M1 Max / Go 1.23.3）。

| Echo v4の条件 | ns/request | B/request | allocs/request |
| --- | ---: | ---: | ---: |
| adapterなし | 177 | 208 | 4 |
| adapter設置・計測無効 | 202 | 256 | 5 |
| 記録有効 | 974 | 961 | 15 |

Echo自体のcontext・response生成も含む値です。Echo3/4の静的ルート表は最初の計測リクエスト時にアプリ・host routerごとにキャッシュします。
静的ルート1個では約971ns、128個では約1,022nsで、どちらも969B・15 allocationsでした。ルート登録はサーバの稼働前に完了させます。

## 実HTTPで試す

```sh
go run ./examples/shop
```

別terminalで:

```sh
./bin/sessionflow collect -target http://127.0.0.1:16060/debug/sessionflow \
  -duration 10s -out artifacts/shop.json &
go run ./examples/shop -load http://127.0.0.1:18080 -users 20 -duration 10s
wait
./bin/sessionflow report -input artifacts/shop.json -out artifacts/shop.html
```

## 検証

`make test`: 本体と`integration/`配下の全モジュールをrace detector + go vetで確認します。個別adapterは依存フレームワークに応じて新しいGo toolchainを取得します。`make test-core`は本体・CLIのみ、`make test-integrations`はadapterのみです。
テスト対象はsession単位sampling、ハッシュ一致、記録上限、匿名の非結合、実行中のstop、panic伝播、HTTP認証、任意のResponseWriter機能の保持、複数sourceの開始時刻順解析、並列・gap除外、CLI複数target収集、部分失敗のrollback、HTMLへの値の安全な埋め込みです。

HTTPレスポンス計測には [httpsnoop](https://github.com/felixge/httpsnoop) を使い、Flusher / Hijacker等の有無を保持します。
[net/http.Request.Pattern](https://pkg.go.dev/net/http#Request) を既定のルート情報に使います。
pprotein本体のUI・APIへの組み込み、ベンチ実行との自動同期、ブラウザ画面遷移の直接計測は今回の実装範囲に含みません。
