<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./docs/logos/mojiokoshin-logo-dark.svg" height="64">
    <source media="(prefers-color-scheme: light)" srcset="./docs/logos/mojiokoshin-logo-light.svg" height="64">
    <img src="./docs/logos/mojiokoshin-logo-light.png" height="64" alt="Mojiokoshin" />
  </picture>
</p>

<p align="center">
  Discordのボイスチャットに最適化された文字起こしシステム。
  <br>
  操作を最小限にし、日常利用で迷わないUXを重視。
</p>

<section align="center">

[![Go](https://img.shields.io/badge/Go-ffffff?style=for-the-badge&labelColor=00add8&logoColor=ffffff&color=f5f5f5&logo=go)](https://go.dev/)
[![Google Cloud Speech-to-Text](https://img.shields.io/badge/Google%20Cloud%20Speech--to%20Text-ffffff?style=for-the-badge&labelColor=4285F4&logoColor=ffffff&color=f5f5f5&logo=google-cloud)](https://cloud.google.com/speech-to-text)

</section>

Mojiokoshin は、Discordのボイスチャットを対象にした文字起こしボットシステムです。
開始・停止の操作を最小限にし、日常利用で迷わないUXを重視しています。

## ✨ 主な機能

- Discord ボイスチャットの文字起こし
- `/mojiokoshi` と `/mojiokoshi-stop` の2つのスラッシュコマンドで操作
- Google Cloud Speech-to-Text 連携
- PostgreSQL へのセッション保存
- 文字起こし結果の Webhook 送信（任意）
- 自動文字起こし開始（`DISCORD_AUTO_TRANSCRIBE` で有効化）

## 🏠 セルフホスト

セルフホストでこのシステムを運用する場合は、 **[Railway](https://railway.com)** へのデプロイを推奨しています。
Mojiokoshin は コンテナ + PostgreSQL で動作するため、Railway上でアプリと DB をまとめて運用できます。

## 🛠️ 開発

### 開発環境のセットアップ

#### 1. 環境変数ファイルを作成

```bash
cp .env.sample .env
```

#### 2. .env に環境変数を設定

| 変数名 | 必須 | デフォルト | 説明 |
| --- | --- | --- | --- |
| `ENV` | No | `production` | 実行環境 |
| `DEFAULT_TRANSCRIBE_LANGUAGE` | No | `ja-JP` | 既定の文字起こし言語コード |
| `MAX_TRANSCRIBE_DURATION_MIN` | No | `120` | 文字起こし最大時間（分） |
| `DATABASE_URL` | Yes | - | PostgreSQL 接続URL |
| `GOOGLE_CLOUD_PROJECT_ID` | Yes | - | Speech-to-Text を利用する Google Cloud プロジェクトID |
| `GOOGLE_CLOUD_CREDENTIALS_JSON` | Yes | - | Google Cloud サービスアカウントJSON |
| `GOOGLE_CLOUD_SPEECH_LOCATION` | No | `asia-northeast1` | Speech-to-Text API のリージョン |
| `GOOGLE_CLOUD_SPEECH_MODEL` | No | `chirp_3` | Speech-to-Text のモデル名 |
| `DISCORD_TOKEN` | Yes | - | Discord Bot Token |
| `DISCORD_GUILD_ID` | Yes | - | コマンドを登録する Discord サーバーID |
| `DISCORD_AUTO_TRANSCRIBE` | No | `false` | 自動文字起こしを有効にするか |
| `DISCORD_AUTO_TRANSCRIBABLE_VC_ID` | No | - | 自動文字起こし対象のボイスチャンネルID（`DISCORD_AUTO_TRANSCRIBE=true` 時は必須） |
| `DISCORD_MESSAGE_SHOW_POWERED_BY` | No | `true` | 文字起こしメッセージに Powered by を表示するか |
| `DISCORD_COUNT_OTHER_BOTS_AS_PARTICIPANTS` | No | `false` | 他ボットを参加者数に含めるか |
| `TRANSCRIPT_TIMEZONE` | No | `Asia/Tokyo` | 文字起こし時刻のタイムゾーン |
| `TRANSCRIPT_WEBHOOK_URL` | No | - | 文字起こし完了時に POST する Webhook URL（設定すると Webhook 通知が有効になる） |

#### 3. 開発コンテナの起動

```bash
make up
```

#### 4. 開発コンテナの停止

```bash
make down
```

## 🔗 Webhook 連携

`TRANSCRIPT_WEBHOOK_URL` を設定すると、文字起こし完了時に `application/json` で POST します。
Webhook を利用して、他サービスと連携できます。

例えば、ご自身で要約用のAPIサーバーを構築すれば、文字起こしデータをすぐに要約して通知できます。

### Payload スキーマ

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `schema_version` | `string` | スキーマバージョン |
| `session_id` | `string` | 文字起こしセッション ID |
| `discord_server_id` | `string` | Discord サーバー ID |
| `discord_server_name` | `string` | Discord サーバー名 |
| `discord_voice_channel_id` | `string` | ボイスチャンネル ID |
| `discord_voice_channel_name` | `string` | ボイスチャンネル名 |
| `start_at` | `string` (RFC3339) | セッション開始時刻 |
| `end_at` | `string` (RFC3339) | セッション終了時刻 |
| `timezone` | `string` | 表示タイムゾーン |
| `duration_seconds` | `number` | 通話時間（秒） |
| `participants` | `string[]` | 参加者表示名の一覧 |
| `participant_details` | `object[]` | 参加者詳細（`user_id`, `display_name`, `is_bot`） |
| `segment_count` | `number` | セグメント数 |
| `transcript_segments` | `object[]` | セグメント詳細（`index`, `start_at`, `end_at`, `transcript`） |
| `transcript` | `string` | 改行連結された全文文字起こし |

### Payload 例

```json
{
  "schema_version": "2026-02-28",
  "session_id": "9d6d86cb-0c9a-4a09-a589-8a1ec1d4f779",
  "discord_server_id": "123456789012345678",
  "discord_server_name": "Example Server",
  "discord_voice_channel_id": "987654321098765432",
  "discord_voice_channel_name": "General",
  "start_at": "2026-02-28T09:00:00+09:00",
  "end_at": "2026-02-28T10:00:00+09:00",
  "timezone": "Asia/Tokyo",
  "duration_seconds": 3600,
  "participants": ["Alice", "Bob"],
  "participant_details": [
    {
      "user_id": "111111111111111111",
      "display_name": "Alice",
      "is_bot": false
    },
    {
      "user_id": "222222222222222222",
      "display_name": "Bob",
      "is_bot": false
    }
  ],
  "segment_count": 2,
  "transcript_segments": [
    {
      "index": 0,
      "start_at": "2026-02-28T09:00:15+09:00",
      "end_at": "2026-02-28T09:01:02+09:00",
      "transcript": "おはようございます"
    },
    {
      "index": 1,
      "start_at": "2026-02-28T09:01:02+09:00",
      "end_at": "2026-02-28T10:00:00+09:00",
      "transcript": "今日の議題を始めます"
    }
  ],
  "transcript": "おはようございます\n今日の議題を始めます"
}
```

## 🤝 コントリビュート

Issue / Pull Request を歓迎します。
大きな変更は、先にIssueで方針を共有してもらえるとレビューがスムーズです。

1. Fork
2. ブランチ作成
3. 実装とテスト
4. Pull Request 作成

不具合報告や改善提案だけでもとても助かります。
皆さんのコントリビュートを心よりお待ちしています。

## ⚖️ ライセンス

[MIT License](./LICENSE)
