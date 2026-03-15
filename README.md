# IoT Web App Sample

React + Go + PostgreSQL + MQTT + Docker Compose + M5Stack Fire を使った IoT Web アプリのサンプルです。

このリポジトリでは、以下をローカルで再現できます。

- Web UI でデバイス一覧、テレメトリ、コマンド履歴を確認
- Go API で MQTT メッセージを受け取り DB に保存
- Mosquitto を使った MQTT Broker 運用
- M5Stack Fire からの `telemetry` / `heartbeat` / `cmd` / `cmd_ack`
- Web UI からの `LED_SET` / `SAMPLING_INTERVAL_SET`

M5Stack Fire 実機で `telemetry` / `heartbeat` / `cmd` / `cmd_ack` の4経路を確認済みです。コマンド履歴の `SENT` / `ACK` / `FAIL` / `TIMEOUT` 更新、および Wi-Fi / MQTT 再接続後の復帰も確認済みです。

## システム構成

- Frontend: React + TypeScript
- Backend: Go (`net/http`)
- Database: PostgreSQL
- Broker: Eclipse Mosquitto
- Infra: Docker Compose
- Device: M5Stack Fire

## 前提環境

このリポジトリは Windows + WSL2 + Docker Desktop を前提にしています。

最低限必要なもの:

1. Windows 11 または Windows 10
2. WSL2
3. Ubuntu などの WSL ディストリビューション
4. Visual Studio Code
5. Git
6. Docker Desktop
7. Arduino IDE

## 1. WSL2 の準備

PowerShell を管理者で開いて、WSL をインストールします。

```powershell
wsl --install
```

インストール後、PC の再起動を求められた場合は再起動してください。

WSL の状態確認:

```powershell
wsl -l -v
```

`VERSION` が `2` になっていれば OK です。

Ubuntu を初回起動したら、ユーザー名とパスワードを設定してください。

## 2. Visual Studio Code の準備

Windows 側に Visual Studio Code をインストールしてください。

- ダウンロード: <https://code.visualstudio.com/>

インストール後、以下の拡張機能を入れておくと扱いやすいです。

- `WSL`
- `Docker`

### VS Code で WSL フォルダを開く

1. VS Code を起動
2. 左下の緑色のアイコンをクリック
3. `Connect to WSL` を選択
4. WSL に接続した状態で `File` -> `Open Folder...` を選ぶ
5. あとで clone したフォルダを開く

### どのターミナルでコマンドを打つか

この README に出てくるコマンドは、基本的に次の2種類です。

- `powershell` と書いてあるもの: Windows の PowerShell で実行
- `bash` と書いてあるもの: WSL の Ubuntu ターミナルで実行

VS Code を WSL 接続で開いている場合、`Terminal` -> `New Terminal` で開くターミナルは通常 `bash` です。以後の `bash` コマンドはそこで実行できます。

## 3. Git の準備とリポジトリ取得

WSL のターミナルで Git を入れます。

```bash
sudo apt update
sudo apt install -y git
```

任意ですが、Git のユーザー設定もしておくと扱いやすいです。

```bash
git config --global user.name "YOUR_NAME"
git config --global user.email "YOUR_EMAIL@example.com"
```

リポジトリを clone します。

```bash
cd ~
git clone https://github.com/iorithe2009/M5stack-IoT-Web-fox.git
cd M5stack-IoT-Web-fox
```

### VS Code で clone 済みフォルダを開く

WSL 接続済みの VS Code で、以下のどちらかで開いてください。

方法 1:

```bash
cd ~/M5stack-IoT-Web-fox
code .
```

方法 2:

- VS Code の `File` -> `Open Folder...`
- `/home/<あなたのユーザー名>/M5stack-IoT-Web-fox` を選択

## 4. Docker Desktop の準備

Windows 側で Docker Desktop をインストールしてください。

- ダウンロード: <https://www.docker.com/products/docker-desktop/>
- インストール後、Docker Desktop を起動
- Settings で WSL Integration を有効化

確認ポイント:

- `Settings` -> `General` で Docker Desktop が起動している
- `Settings` -> `Resources` -> `WSL Integration` で使用するディストリビューションを有効化

WSL 側で Docker が使えるか確認します。

```bash
docker --version
docker compose version
docker ps
```

エラーが出なければ準備完了です。

## 5. アプリの起動

リポジトリ直下で以下を実行します。

```bash
docker compose up --build
```

初回はイメージのビルドに少し時間がかかります。

起動後のアクセス先:

- Web: <http://localhost:3000>
- API 動作確認: <http://localhost:8080/api/hello>

バックグラウンドで起動したい場合:

```bash
docker compose up -d --build
```

停止:

```bash
docker compose down
```

### VS Code からの起動と確認

VS Code のターミナルで上の `docker compose up --build` を実行できます。

起動後、VS Code を使っている場合は Docker 拡張機能の Containers 一覧から対象コンテナを右クリックして `Open in Browser` を使うと開きやすいです。

## 6. 動作確認

まずは API が応答するか確認します。

```bash
curl http://localhost:8080/api/hello
```

デバイス一覧 API 確認:

```bash
curl http://localhost:8080/api/devices
```

## API エンドポイント

| メソッド | URL | 機能 |
| ------- | --- | ---- |
| GET | `/api/hello` | 文字列を返す |
| GET | `/api/messages` | DB のメッセージ一覧 |
| POST | `/api/messages` | DB にメッセージ追加 |
| GET | `/api/devices` | IoT デバイス一覧（online 状態・最新値） |
| GET | `/api/devices/{device_key}/telemetry` | テレメトリ履歴（`?metric=temp&duration=1h`） |
| POST | `/api/devices/{device_key}/commands` | コマンド送信（`LED_SET` / `SAMPLING_INTERVAL_SET`） |
| GET | `/api/devices/{device_key}/commands` | コマンド履歴取得（`?limit=20`） |
| WS | `/ws` | リアルタイム Push（`device_state_changed` / `device_event` / `command_updated`） |

## 7. MQTT の疎通テスト

M5Stack がなくても、まずは MQTT publish で API 連携を確認できます。

### telemetry を送る

```bash
docker compose exec mqtt mosquitto_pub -h localhost -p 1883 \
  -t "iot/m5-001/telemetry" \
  -m '{"ts":"2026-03-09T12:00:00Z","metrics":{"temp":26.1,"ax":0.02},"unit":{"temp":"C","ax":"g"}}'
```

### heartbeat を送る

```bash
docker compose exec mqtt mosquitto_pub -h localhost -p 1883 \
  -t "iot/m5-001/heartbeat" \
  -m '{"ts":"2026-03-09T12:00:00Z","fw":"1.0.0","ip":"192.168.0.10"}'
```

### コマンド送信 API を叩く

```bash
curl -X POST http://localhost:8080/api/devices/m5-001/commands \
  -H "Content-Type: application/json" \
  -d '{"commandType":"LED_SET","payload":{"ledOn":true}}'
```

### コマンド履歴確認

```bash
curl http://localhost:8080/api/devices/m5-001/commands?limit=20
```

## 8. M5Stack Fire ファームウェアの書き込み

### Arduino IDE の準備

1. [Arduino IDE](https://www.arduino.cc/en/software) をインストール
2. Arduino IDE のボードマネージャ URL に以下を追加

```text
https://m5stack.oss-cn-shenzhen.aliyuncs.com/resource/arduino/package_m5stack_index.json
```

3. ライブラリマネージャから以下をインストール

- `M5Unified`
- `Adafruit NeoPixel`
- `PubSubClient`
- `ArduinoJson`

### `secrets.h` の作成

`m5_stack/secrets.h.example` をコピーして `m5_stack/secrets.h` を作成します。

```bash
cp m5_stack/secrets.h.example m5_stack/secrets.h
```

`secrets.h` を開いて以下を設定してください。

| 項目 | 説明 |
| ---- | ---- |
| `WIFI_SSID` | 接続する Wi-Fi の SSID |
| `WIFI_PASSWORD` | Wi-Fi のパスワード |
| `MQTT_HOST` | Windows ホストの LAN IP アドレス |
| `MQTT_PORT` | MQTT ポート。通常は `1883` |
| `DEVICE_KEY` | サーバー側と一致させるデバイス ID。例: `m5-001` |

### `MQTT_HOST` の設定で重要な点

M5Stack は LAN 上の外部デバイスなので、`MQTT_HOST` には WSL2 の `172.x.x.x` ではなく、Windows ホストの LAN IP を設定してください。

Windows 側で確認:

```powershell
ipconfig
```

出力例:

```text
IPv4 Address. . . . . . . . . . . : 192.168.11.20
```

この `192.168.11.20` を `MQTT_HOST` に設定します。

### 書き込み手順

1. `m5_stack/phase4_m5stack_fire_sample.ino` を Arduino IDE で開く
2. ボードを `M5Stack-Fire` に設定
3. M5Stack Fire を USB 接続する
4. 正しいシリアルポートを選ぶ
5. 書き込みを実行する

### M5Stack 側の機能

- `telemetry` を 10 秒ごとに送信
- `heartbeat` を 5 秒ごとに送信
- `LED_SET` を受信して LED を ON/OFF
- `SAMPLING_INTERVAL_SET` を受信して送信周期を変更
- `cmd_ack` を返却
- `BtnA`: LED ローカルトグル
- `BtnB`: telemetry 即時送信
- `BtnC`: heartbeat 即時送信

## 9. よくある詰まりどころ

### Docker が WSL から使えない

- Docker Desktop が起動しているか確認
- WSL Integration が有効か確認
- `docker ps` が動くか確認

### M5Stack から MQTT につながらない

- `MQTT_HOST` に WSL の IP を入れていないか確認
- Windows ホストの LAN IP を設定しているか確認
- M5Stack と PC が同じネットワークにいるか確認
- `1883` ポートにアクセスできるか確認

### Web は開くがデータが出ない

- `docker compose logs api` を確認
- MQTT テスト publish を実行して DB / API まで流れるか確認
- `device_key` が `m5-001` で揃っているか確認

## 10. 補足

- `m5_stack/secrets.h` は `.gitignore` で除外されています
- この公開版リポジトリには内部設計メモや作業計画書は含めていません
- 公開内容は再現用サンプルと実装コードを中心に整理しています
