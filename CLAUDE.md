# jitter

テキスト+フォントを入力し、1文字ごとにランダムな「癖」変形を加えて出力するCLIツール。

## ビルド・テスト

```bash
go build
go test ./...
gofmt -l .
go vet ./...
```

## アーキテクチャ

- `main.go`: CLIエントリーポイント、サブコマンド定義（pflag）
- `font.go`: フォント読み込み、グリフアウトライン抽出（go-text/typesetting）
- `jitter.go`: 変形エンジン（回転・位置ずれ・拡縮・シアー・ストローク太さ・点揺らぎをグリフ単位で適用）
- `layout.go`: ページ寸法・座標変換
- `svg.go`: SVG出力（フォント座標→SVG座標変換）
- `png.go`: PNG出力（x/image/vector ラスタライザ、透明背景）
- `bake.go`: bakeモード本体（交互グリフ生成、テーブル再構築）
- `sfnt.go`: SFNT/TTCリーダー、テーブルビルダー、チェックサム付きライター
- `glyf.go`: TrueType `glyf`/`loca` 構築、3次→2次ベジェ変換
- `gsub.go`: `calt` GSUB構築（Extension + ChainContextSubst + SingleSubst）
- render モード: テキスト+フォント → SVG/PNG
- bake モード: TTF/OTF フォント → calt付き TTF

## 技術ルール

- コミットメッセージに Co-Authored-By を付けない
