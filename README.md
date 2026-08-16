# kubeconfig-merge

複数の kubeconfig（主に RKE2 が出力するもの）から必要な context だけを選び、
cluster / user / context 名や server を書き換えて 1 つの `config` にまとめる CLI ツールです。

これまで手作業で行っていた

```bash
kubectl config rename-context default cluster-merino-admin
KUBECONFIG=a.yaml:b.yaml kubectl config view --flatten > config
```

を、`kconfig.yaml` による宣言的・再現可能な操作に置き換えます。
Go の単一バイナリなので、Python や PyYAML などのランタイム依存はありません。

## インストール

[GitHub Releases](../../releases) の tar.gz を展開して `~/.kube` に置くだけで使えます。

```bash
VERSION=1.0.0
curl -L -o kubeconfig-merge.tar.gz \
  https://github.com/zinntikumugai/kubeconfig-merge/releases/download/v${VERSION}/kubeconfig-merge_${VERSION}_linux_amd64.tar.gz
tar xzf kubeconfig-merge.tar.gz -C ~/.kube kubeconfig-merge
cd ~/.kube && ./kubeconfig-merge --version
```

アーカイブは `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` の 4 種類、
`sha256sums.txt` も同時に公開されます。

## ディレクトリ構成

本ツールは **カレントディレクトリ** を作業ディレクトリとして動作します（通常は `~/.kube`）。

```
~/.kube/
├── kubeconfig-merge          # 本体（展開したバイナリ）
├── kconfig.yaml              # マージ定義（このツールの設定ファイル）
├── merino.kconfig.yaml       # 入力 kubeconfig（source ID = merino）
├── kikyo.kconfig.yml         # 入力 kubeconfig（source ID = kikyo）
├── config                    # 出力（kubectl が読む kubeconfig, 0600）
└── backup/
    └── config.20260817-013100   # 上書き前の config のコピー（0600）
```

### ファイル命名規則

| ファイル | 役割 |
|---|---|
| `kconfig.yaml` または `kconfig.yml` | マージ定義。どちらか一方だけを置く |
| `<source>.kconfig.yaml` / `<source>.kconfig.yml` | 入力 kubeconfig。`<source>` が `kconfig.yaml` の `sources` のキー（source ID）になる |
| `config` | 出力。入力として読み込まれることはない |

`sources` に `merino` と書けば `merino.kconfig.yaml`（または `merino.kconfig.yml`）を探します。
同じ source ID で `.yaml` と `.yml` の両方が存在する場合はエラーです。

## kconfig.yaml

```yaml
version: 1                      # 1 のみ受理

options:
  flatten: false                # 省略時 false

sources:                        # キー = source ID = <id>.kconfig.yaml のファイル名部分
  merino:
    contexts:                   # 1 ファイルから N 個の context を選べる
      - source: kubernetes-admin@kubernetes   # 入力側の contexts[].name（必須）
        profile: merino-prod                  # 適用する profile 名（必須）
      - source: staging-admin@kubernetes
        profile: merino-stg

  kikyo:
    contexts:
      - source: default
        profile: kikyo-prod

profiles:                       # 選択した context 1 つにつき profile 1 つ
  merino-prod:
    cluster:
      name: cluster-merino                    # 必須 → 出力の clusters[].name
      server: https://172.16.1.100:6443       # 任意（省略時は元の値を保持）
    user:
      name: cluster-merino-admin              # 必須 → 出力の users[].name
    context:
      name: cluster-merino-admin              # 必須 → 出力の contexts[].name

  merino-stg:
    cluster:
      name: cluster-merino-stg
      server: https://172.16.1.101:6443
    user:
      name: cluster-merino-stg-admin
    context:
      name: cluster-merino-stg-admin

  kikyo-prod:
    cluster:
      name: cluster-kikyo                     # server 省略 = 元の kubeconfig の値のまま
    user:
      name: cluster-kikyo-admin
    context:
      name: cluster-kikyo-admin

current-context: cluster-merino-admin   # 任意。指定した場合は出力に存在必須
```

書き換えられるのは `cluster.name` / `cluster.server` / `user.name` / `context.name` の 4 つだけです。
証明書・トークン等はそのままコピーされます。

## 実行

```bash
cd ~/.kube
./kubeconfig-merge
```

```
SOURCE  SOURCE CONTEXT               OUTPUT CONTEXT            CLUSTER             SERVER
kikyo   default                      cluster-kikyo-admin       cluster-kikyo       https://127.0.0.1:6443
merino  kubernetes-admin@kubernetes  cluster-merino-admin      cluster-merino      https://172.16.1.100:6443
merino  staging-admin@kubernetes     cluster-merino-stg-admin  cluster-merino-stg  https://172.16.1.101:6443

backed up the previous config to /home/user/.kube/backup/config.20260817-013100
wrote /home/user/.kube/config (3 contexts)
```

処理・表示順は source ID の名前順、その中は `contexts` の記述順です。

### フラグ

| フラグ | 説明 |
|---|---|
| `--dry-run` | 検証と結果表示だけを行い、ファイルを一切変更しない |
| `--flatten` | 証明書・鍵ファイルを data として埋め込む（`options.flatten` より優先） |
| `--no-backup` | 既存 `config` の backup を作らずに上書きする |
| `--verbose` | 読み込み・解決・書き込みの経過を stderr に出力する（鍵やトークンの値は出力しない） |
| `--version` | バージョンを表示して終了する |
| `--help` | 使い方を表示して終了する |

終了コードは 正常 `0` / エラー `1` / フラグの指定ミス `2` です。
`--dry-run` でも検証に失敗すれば `1` になります。

### --dry-run

```bash
./kubeconfig-merge --dry-run
```

```
SOURCE  SOURCE CONTEXT               OUTPUT CONTEXT            CLUSTER             SERVER
kikyo   default                      cluster-kikyo-admin       cluster-kikyo       https://127.0.0.1:6443
merino  kubernetes-admin@kubernetes  cluster-merino-admin      cluster-merino      https://172.16.1.100:6443
merino  staging-admin@kubernetes     cluster-merino-stg-admin  cluster-merino-stg  https://172.16.1.101:6443

dry-run: /home/user/.kube/config was not modified
```

検証（context / cluster / user の参照、名前の重複、`current-context` の存在）は
すべて書き込み前に完了するため、エラーになった場合は既存の `config` に一切触れません。

### backup

`config` が既に存在する場合、上書きの前に `backup/config.<YYYYMMDD-HHMMSS>` へコピーします。
同じ秒に複数回実行してファイル名が衝突したときは `-1`, `-2` … が付き、既存の backup を上書きすることはありません。
`--no-backup` で無効化できます。

### flatten

`flatten: true`（または `--flatten`）にすると、`certificate-authority` / `client-certificate` / `client-key`
といった**ファイル参照が読み込まれ、`*-data` として出力に埋め込まれます**。
出力 `config` 1 ファイルだけを別マシンへ持っていける形になります。

flatten は **各 source を読み込んだ直後**（マージ前）に実行します。
ファイル参照は「その kubeconfig ファイルの位置」を基準とした相対パスであり、
マージ後にまとめて解決するとどのファイル由来かが失われて壊れるためです。
同じ理由で、flatten の有無にかかわらず読み込み直後に相対パスを絶対パスへ解決しています。

## RKE2 kubeconfig の例

RKE2 が出力する `/etc/rancher/rke2/rke2.yaml` は cluster / user / context がすべて `default`、
server が `https://127.0.0.1:6443` です。これをそのまま `merino.kconfig.yaml` として置きます。

```yaml
apiVersion: v1
kind: Config
clusters:
- name: default
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority-data: ZHVtbXktY2E=
users:
- name: default
  user:
    client-certificate-data: ZHVtbXktY2xpZW50LWNlcnQ=
    client-key-data: ZHVtbXktY2xpZW50LWtleQ==
contexts:
- name: default
  context:
    cluster: default
    user: default
current-context: default
preferences: {}
```

`default` という名前と `127.0.0.1` を、クラスタ名と実 IP に付け替えます。

```yaml
version: 1

sources:
  merino:
    contexts:
      - source: default            # RKE2 の context 名は "default"
        profile: merino-prod

profiles:
  merino-prod:
    cluster:
      name: cluster-merino
      server: https://172.16.1.100:6443   # 127.0.0.1 を実アドレスへ
    user:
      name: cluster-merino-admin
    context:
      name: cluster-merino-admin          # default をリネーム

current-context: cluster-merino-admin
```

複数台の RKE2 を扱う場合も、`<source>.kconfig.yaml` を並べて
それぞれ別の profile を当てれば、`default` 同士の名前衝突なくマージできます。

## 注意事項

- **出力 `config` と backup は常に 0600 で作成されます。** 既存ファイルの mode は引き継ぎません
  （誤って 0644 だった場合に秘密が漏れ続けるのを防ぐため）。書き込みは一時ファイル → rename の
  アトミック置換です。
- **相対パスは絶対パスに解決されます。** 入力 kubeconfig 中の `certificate-authority: ./certs/ca.crt`
  などは、その kubeconfig の位置を基準に絶対パス化されて出力されます。
- **`flatten: false` のとき、参照先の cert ファイルが存在しないと検証エラーになります**
  （client-go の検証が実際にファイルを開くため）。`flatten: true` にすれば解消します。
- **未使用の profile は無視されます**（エラーにはなりません。`--verbose` で確認できます）。
- **`kconfig.yaml` と `kconfig.yml` の両方があるとエラーです。** 同様に、
  同じ source ID で `<id>.kconfig.yaml` と `<id>.kconfig.yml` の両方があってもエラーです。
- **`kconfig.yaml` の未知フィールドはエラーです。** typo（例: `profil:`）を黙って無視せず、
  `unknown field "profil"` として報告します。
- 出力先の `config` が入力として読み込まれることはありません（入力は `*.kconfig.yaml|yml` のみ）。
- **出力の並び順は cluster / user / context 名の昇順で固定です**（client-go の変換がキーをソートするため）。
  同じ入力なら毎回同じ内容になるので、`config` を差分で確認できます。なお空の `preferences` は
  出力されません（kubeconfig 上 optional で、kubectl も問題なく読めます）。

## ビルド

Go のバージョンは `mise.toml` で 1.26.6 に固定しています。

```bash
mise install
make build            # ./kubeconfig-merge を生成（CGO_ENABLED=0, -trimpath, バージョン埋め込み）
make test             # go test -race ./...
make vet              # go vet ./...
make e2e              # ビルド済みバイナリでの e2e（scripts/e2e.sh）
make dist             # 4 プラットフォーム分の tar.gz + sha256sums.txt を dist/ に生成
make clean
```

`go` が PATH に無い環境では Makefile が自動的に `mise exec -- go` へフォールバックします。
明示的に指定する場合は `make build GO="mise exec -- go"` としてください。

Makefile を使わない場合:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o kubeconfig-merge .
```

バージョンはソースに書かず `-ldflags` で埋め込みます。`--version` の表示は次の形式です。

```
kubeconfig-merge v1.0.0 (go1.26.6 linux/amd64)
```

## リリース手順

タグを push するだけです。GitHub Actions が vet + test を実行し、
通ったら 4 プラットフォーム分の tar.gz と `sha256sums.txt` を Release に添付します。

```bash
git tag v1.0.0
git push --tags
```

アーカイブ名は先頭の `v` を除いた `kubeconfig-merge_1.0.0_linux_amd64.tar.gz` 形式です。

## 将来対応

- `--directory` : カレントディレクトリ以外を作業ディレクトリに指定する
- `tls-server-name` の上書き
- `namespace` の指定
