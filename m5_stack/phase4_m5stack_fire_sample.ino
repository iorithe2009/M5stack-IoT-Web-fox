#include <M5Unified.h>
#include <Adafruit_NeoPixel.h>
#include <WiFi.h>
#include <PubSubClient.h>
#include <ArduinoJson.h>
#include <time.h>
#include "secrets.h"

// ============================================================
// M5Stack Fire -> MQTT テレメトリ / ハートビート / コマンド / ACK送信
// Phase 4 MVP サンプル
//
// 必要ライブラリ:
// - M5Unified
// - Adafruit NeoPixel (LEDストリップ制御用 ※FastLEDはIDF v5.xのRMTドライバと競合するため使用不可)
// - PubSubClient
// - ArduinoJson
//
// 注意事項:
// - このサンプルはローカルMVP接続の動作確認を優先した実装です。
// - PubSubClient は軽量で扱いやすいですが、publish の QoS は実質 QoS 0 です。
//   cmd/cmd_ack で厳密な QoS 1 が必要になった場合は MQTT ライブラリを差し替えてください。
// - Wi-Fi / MQTT の認証情報は secrets.h に記述してください。
//   secrets.h は .gitignore で除外されています。secrets.h.example を参考に作成してください。
// ============================================================

// ------------------------
// LED ストリップ設定（M5Stack Fire: SK6812 x 10個, GPIO 15）
// SK6812 は RGBW 4ch だが白チャンネルは 0 固定で RGB のみ使用する
// ------------------------
#define LED_PIN  15
#define NUM_LEDS 10
Adafruit_NeoPixel strip(NUM_LEDS, LED_PIN, NEO_GRBW + NEO_KHZ800);

// ------------------------
// デバイス / トピック設定
// ------------------------
static const char* FW_VERSION = "0.1.0";   // ファームウェアバージョン（ハートビートで通知）

// MQTTトピック文字列（setup()内で DEVICE_KEY を組み込んで生成）
String topicTelemetry;
String topicHeartbeat;
String topicCmd;
String topicCmdAck;

// ------------------------
// タイミング設定（ミリ秒）
// ------------------------
unsigned long telemetryIntervalMs = 10000;  // テレメトリ送信間隔（デフォルト10秒、コマンドで変更可）
const unsigned long heartbeatIntervalMs = 5000;   // ハートビート送信間隔（5秒）
const unsigned long mqttRetryIntervalMs = 3000;   // MQTT再接続リトライ間隔（3秒）
const unsigned long wifiRetryIntervalMs = 5000;   // WiFi再接続リトライ間隔（5秒）
const unsigned long statusDrawIntervalMs = 1000;  // 画面更新間隔（1秒）

// UNIX時刻の最小有効値（NTP同期前の誤った時刻を除外するための閾値）
// 2024-01-01T00:00:00Z = 1704067200
const time_t minValidUnixTime = 1704067200;

// 各処理の最終実行時刻（millis()の値を保持）
unsigned long lastTelemetryMs = 0;
unsigned long lastHeartbeatMs = 0;
unsigned long lastMqttRetryMs = 0;
unsigned long lastWifiRetryMs = 0;
unsigned long lastStatusDrawMs = 0;

// ------------------------
// 実行時の状態管理
// ------------------------
WiFiClient wifiClient;
PubSubClient mqttClient(wifiClient);

bool ledOn = false;        // LED（ディスプレイピクセル）の現在の点灯状態
bool lastImuValid = false; // IMUデータが有効かどうかのフラグ
float lastAx = 0.0f;       // 加速度センサ X軸（単位: g）
float lastAy = 0.0f;       // 加速度センサ Y軸（単位: g）
float lastAz = 0.0f;       // 加速度センサ Z軸（単位: g）
float lastTemp = 0.0f;     // 内部温度センサの値（単位: ℃）
String lastStatusLine = "booting...";  // 画面に表示する最新ステータス文字列
String lastCmdLine = "-";              // 最後に受信・実行したコマンドの表示用文字列
bool ntpStarted = false;   // NTP時刻同期を開始済みかどうかのフラグ（重複起動防止）

// ============================================================
// ユーティリティ関数
// ============================================================

/**
 * 現在のUTC時刻をISO 8601形式（"YYYY-MM-DDTHH:MM:SSZ"）の文字列に変換して返す。
 * NTP同期が完了していない場合や時刻が無効な場合は false を返す。
 */
bool iso8601NowUtc(String& out) {
  char buf[32];
  time_t now = time(nullptr);

  // NTP未同期などで時刻が最小有効値を下回っている場合は無効とみなす
  if (now < minValidUnixTime) {
    return false;
  }

  struct tm utc;
  if (gmtime_r(&now, &utc) == nullptr) {
    return false;
  }

  if (strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%SZ", &utc) == 0) {
    return false;
  }

  out = String(buf);
  return true;
}

/**
 * JSONドキュメントに現在のUTCタイムスタンプ("ts")を追加する。
 * NTP未同期の場合は追加しない（tsなしのまま送信）。
 */
template <typename TDocument>
void addCurrentTsIfAvailable(TDocument& doc) {
  String ts;
  if (iso8601NowUtc(ts)) {
    doc["ts"] = ts;
  }
}

/**
 * WiFi接続済みかつNTP未起動の場合、NTP時刻同期を開始する。
 * 一度起動すれば以後は何もしない（ntpStarted フラグで管理）。
 */
void ensureTimeSync() {
  if (ntpStarted || !WiFi.isConnected()) return;

  // NTPサーバーを複数指定してUTC（オフセット0）で同期する
  configTime(0, 0, "pool.ntp.org", "time.google.com", "ntp.nict.jp");
  ntpStarted = true;
}

/**
 * M5Stack FireのLEDストリップ（SK6812 x 10個）を一括で点灯/消灯する。
 * ON: 緑(R=0, G=255, B=0, W=0) / OFF: 消灯
 */
void applyLedState(bool on) {
  ledOn = on;
  uint32_t color = on ? strip.Color(0, 255, 0, 0) : strip.Color(0, 0, 0, 0);
  strip.fill(color);
  strip.show();
}

/**
 * LCDに現在の接続状態・センサ値・ステータスを表示する。
 * loop() から1秒ごとに呼び出される。
 */
void drawStatus() {
  M5.Display.fillScreen(TFT_BLACK);
  M5.Display.setCursor(10, 10);
  M5.Display.setTextSize(2);
  M5.Display.setTextColor(TFT_WHITE, TFT_BLACK);

  M5.Display.printf("Phase4 M5 Fire\n");
  M5.Display.printf("WiFi: %s\n", WiFi.isConnected() ? "OK" : "NG");
  M5.Display.printf("MQTT: %s\n", mqttClient.connected() ? "OK" : "NG");
  M5.Display.printf("IP  : %s\n", WiFi.localIP().toString().c_str());
  M5.Display.printf("LED : %s\n", ledOn ? "ON" : "OFF");
  M5.Display.printf("Int : %lu ms\n", telemetryIntervalMs);  // テレメトリ送信間隔

  M5.Display.printf("\nTemp: %.2f C\n", lastTemp);
  M5.Display.printf("AX: %.3f\n", lastAx);
  M5.Display.printf("AY: %.3f\n", lastAy);
  M5.Display.printf("AZ: %.3f\n", lastAz);

  M5.Display.printf("\nStatus:\n%s\n", lastStatusLine.c_str());
  M5.Display.printf("\nLast CMD:\n%s\n", lastCmdLine.c_str());
}

/**
 * ステータス文字列を更新し、シリアルモニタにも出力する。
 */
void setStatus(const String& s) {
  lastStatusLine = s;
  Serial.println("[STATUS] " + s);
}

// ============================================================
// センサ読み取り
// ============================================================

/**
 * IMU（加速度センサ）と内部温度センサの値を読み取り、グローバル変数に格納する。
 *
 * M5Unified の IMU API は float (g単位) で値を返すため、手動スケーリング不要。
 * M5.Imu.update() で最新データを取得し、getImuData() で値を参照する。
 *
 * 【注意】temperatureRead() はESP32チップの内部温度であり、室温センサではありません。
 * MVPのデータ疎通確認には十分ですが、実際の環境温度とは異なります。
 */
void updateSensors() {
  // IMUデータを更新して加速度を取得（戻り値: 更新されたデータ種別ビットフラグ）
  if (M5.Imu.update()) {
    auto data = M5.Imu.getImuData();
    lastAx = data.accel.x;  // 単位: g（スケーリング済み）
    lastAy = data.accel.y;
    lastAz = data.accel.z;
    lastImuValid = true;
  }

  // ESP32内部温度センサの値を読み取る
  lastTemp = temperatureRead();
}

// ============================================================
// MQTT Publish
// ============================================================

/**
 * JsonDocument をシリアライズしてMQTTでパブリッシュする。
 * @param topic  送信先トピック
 * @param doc    送信するJSONドキュメント
 * @return 送信成功なら true
 */
bool publishJson(const String& topic, const JsonDocument& doc) {
  char payload[512];
  size_t n = serializeJson(doc, payload, sizeof(payload));
  if (n == 0) {
    Serial.println("[MQTT] serialize failed");
    return false;
  }
  bool ok = mqttClient.publish(topic.c_str(), payload);
  Serial.printf("[MQTT] PUB %s => %s\n", topic.c_str(), ok ? "OK" : "NG");
  if (!ok) {
    // 送信失敗時はペイロードをシリアルに出力してデバッグに役立てる
    Serial.println(payload);
  }
  return ok;
}

/**
 * ハートビートを送信する。
 * サーバー側でデバイスの死活監視に使用される。
 * @param immediate  true の場合は復旧直後の即時送信とみなしステータス表示を変える
 */
void publishHeartbeat(bool immediate = false) {
  StaticJsonDocument<192> doc;
  addCurrentTsIfAvailable(doc);
  doc["fw"] = FW_VERSION;                        // ファームウェアバージョン
  doc["ip"] = WiFi.localIP().toString();         // 現在のIPアドレス

  if (publishJson(topicHeartbeat, doc)) {
    lastHeartbeatMs = millis();
    setStatus(immediate ? "heartbeat published (recovery)" : "heartbeat published");
  }
}

/**
 * センサデータをテレメトリとしてMQTTでパブリッシュする。
 * 送信前に updateSensors() でセンサ値を最新化する。
 */
void publishTelemetry() {
  updateSensors();

  StaticJsonDocument<320> doc;
  addCurrentTsIfAvailable(doc);

  // metrics: 各センサの数値（小数点以下の桁数を固定して文字列として埋め込む）
  // serialized() を使うことで ArduinoJson が数値を引用符なしのJSONナンバーとして出力する
  JsonObject metrics = doc.createNestedObject("metrics");
  metrics["temp"] = serialized(String(lastTemp, 2));  // 小数点2桁
  metrics["ax"]   = serialized(String(lastAx, 3));    // 小数点3桁
  metrics["ay"]   = serialized(String(lastAy, 3));
  metrics["az"]   = serialized(String(lastAz, 3));

  // unit: 各メトリクスの単位
  JsonObject unit = doc.createNestedObject("unit");
  unit["temp"] = "C";
  unit["ax"]   = "g";
  unit["ay"]   = "g";
  unit["az"]   = "g";

  if (publishJson(topicTelemetry, doc)) {
    lastTelemetryMs = millis();
    setStatus("telemetry published");
  }
}

/**
 * コマンドACKをサーバーへ送信する。
 * @param requestId  受信したコマンドの requestId（紐付け用）
 * @param status     処理結果 ("ok" または "error")
 * @param message    人間可読なメッセージ
 */
void publishAck(const String& requestId, const String& status, const String& message) {
  StaticJsonDocument<256> doc;
  doc["requestId"] = requestId;
  addCurrentTsIfAvailable(doc);
  doc["status"]  = status;
  doc["message"] = message;

  if (publishJson(topicCmdAck, doc)) {
    setStatus("cmd_ack published: " + status);
  }
}

// ============================================================
// コマンド処理
// ============================================================

/**
 * LED_SET コマンドを処理する。
 * ペイロードの "ledOn" (bool) に従ってLEDを点灯/消灯し、ACKを返す。
 */
void handleLedSet(const JsonVariantConst payload, const String& requestId) {
  if (!payload.containsKey("ledOn")) {
    publishAck(requestId, "error", "invalid payload: ledOn required");
    return;
  }

  bool on = payload["ledOn"].as<bool>();
  applyLedState(on);
  lastCmdLine = String("LED_SET ledOn=") + (on ? "true" : "false");
  publishAck(requestId, "ok", on ? "LED ON" : "LED OFF");
}

/**
 * SAMPLING_INTERVAL_SET コマンドを処理する。
 * ペイロードの "seconds" (int, 1〜3600) に従ってテレメトリ送信間隔を変更し、ACKを返す。
 */
void handleSamplingIntervalSet(const JsonVariantConst payload, const String& requestId) {
  if (!payload.containsKey("seconds")) {
    publishAck(requestId, "error", "invalid payload: seconds required");
    return;
  }

  int seconds = payload["seconds"].as<int>();
  // 1秒未満・1時間超えは無効として弾く
  if (seconds < 1 || seconds > 3600) {
    publishAck(requestId, "error", "invalid payload: seconds out of range");
    return;
  }

  telemetryIntervalMs = static_cast<unsigned long>(seconds) * 1000UL;
  lastCmdLine = "SAMPLING_INTERVAL_SET seconds=" + String(seconds);
  publishAck(requestId, "ok", "sampling interval updated");
}

/**
 * MQTTメッセージ受信コールバック。PubSubClientから呼び出される。
 * コマンドトピックへのメッセージをパースし、対応するハンドラに振り分ける。
 */
void onMqttMessage(char* topic, byte* payload, unsigned int length) {
  String incomingTopic = String(topic);

  // byte配列をString に変換
  String body;
  body.reserve(length + 1);
  for (unsigned int i = 0; i < length; ++i) {
    body += static_cast<char>(payload[i]);
  }

  Serial.printf("[MQTT] SUB %s\n", incomingTopic.c_str());
  Serial.println(body);

  // JSONパース
  StaticJsonDocument<384> doc;
  DeserializationError err = deserializeJson(doc, body);
  if (err) {
    lastCmdLine = "parse error";
    publishAck("", "error", "invalid json");
    return;
  }

  // requestId と commandType を取り出す（存在しない場合は空文字）
  String requestId   = doc["requestId"]   | "";
  String commandType = doc["commandType"] | "";

  // requestId は必須（ACKの紐付けに使用）
  if (requestId.isEmpty()) {
    publishAck("", "error", "requestId required");
    return;
  }

  // コマンド種別に応じてハンドラへ振り分け
  if (commandType == "LED_SET") {
    handleLedSet(doc["payload"], requestId);
    return;
  }

  if (commandType == "SAMPLING_INTERVAL_SET") {
    handleSamplingIntervalSet(doc["payload"], requestId);
    return;
  }

  // 未知のコマンド
  lastCmdLine = "unknown command: " + commandType;
  publishAck(requestId, "error", "unknown command");
}

// ============================================================
// 接続管理
// ============================================================

/**
 * WiFi接続を維持する。未接続かつリトライ間隔が経過している場合に再接続を試みる。
 * 接続済みの場合はNTP時刻同期も確認する。
 */
void ensureWiFi() {
  if (WiFi.isConnected()) {
    ensureTimeSync();
    return;
  }

  unsigned long now = millis();
  if (now - lastWifiRetryMs < wifiRetryIntervalMs) return;
  lastWifiRetryMs = now;

  setStatus("connecting WiFi...");
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
}

/**
 * コマンドトピックを購読する。
 * MQTT接続直後に呼び出される。
 */
void subscribeCommandTopic() {
  bool ok = mqttClient.subscribe(topicCmd.c_str());
  Serial.printf("[MQTT] SUBSCRIBE %s => %s\n", topicCmd.c_str(), ok ? "OK" : "NG");
}

/**
 * MQTT接続を維持する。WiFi未接続・接続済みの場合は何もしない。
 * 切断中かつリトライ間隔が経過している場合に再接続を試みる。
 * 再接続成功時はコマンドトピックを購読し、復旧ハートビートを送信する。
 */
void ensureMqtt() {
  if (!WiFi.isConnected()) return;
  if (mqttClient.connected()) return;

  unsigned long now = millis();
  if (now - lastMqttRetryMs < mqttRetryIntervalMs) return;
  lastMqttRetryMs = now;

  setStatus("connecting MQTT...");

  // クライアントIDは "m5fire-{DEVICE_KEY}" 形式とする
  String clientId = String("m5fire-") + DEVICE_KEY;

  bool ok;
  if (strlen(MQTT_USER) > 0) {
    // 認証あり接続
    ok = mqttClient.connect(clientId.c_str(), MQTT_USER, MQTT_PASSWORD);
  } else {
    // 匿名接続
    ok = mqttClient.connect(clientId.c_str());
  }

  if (!ok) {
    Serial.printf("[MQTT] connect failed rc=%d\n", mqttClient.state());
    setStatus("MQTT connect failed");
    return;
  }

  setStatus("MQTT connected");
  subscribeCommandTopic();
  publishHeartbeat(true);  // 復旧直後のハートビートを即時送信
}

// ============================================================
// Arduino エントリーポイント
// ============================================================

/**
 * 初期化処理。電源投入・リセット時に一度だけ実行される。
 */
void setup() {
  // M5Unified 初期化（ボードを自動検出・LCD/IMU/電源ICすべてを初期化）
  M5.begin();
  Serial.begin(115200);

  // LCD初期設定
  M5.Display.setTextSize(2);
  M5.Display.setTextColor(TFT_WHITE, TFT_BLACK);
  M5.Display.fillScreen(TFT_BLACK);

  // LEDストリップ初期化（SK6812, GPIO 15, 10個）
  strip.begin();
  strip.setBrightness(50);
  strip.show();  // 全消灯で初期化

  // MQTTトピック文字列を DEVICE_KEY を使って生成
  topicTelemetry = String("iot/") + DEVICE_KEY + "/telemetry";
  topicHeartbeat = String("iot/") + DEVICE_KEY + "/heartbeat";
  topicCmd       = String("iot/") + DEVICE_KEY + "/cmd";
  topicCmdAck    = String("iot/") + DEVICE_KEY + "/cmd_ack";

  // MQTTクライアント設定
  mqttClient.setServer(MQTT_HOST, MQTT_PORT);
  mqttClient.setCallback(onMqttMessage);
  mqttClient.setBufferSize(1024);  // デフォルト(256B)だと大きめのJSONで切れるため拡張

  applyLedState(false);  // 起動時はLED消灯
  setStatus("boot complete");
  drawStatus();
}

/**
 * メインループ。繰り返し実行される。
 * ボタン操作・WiFi/MQTT接続維持・定期送信・画面更新を担当する。
 */
void loop() {
  M5.update();  // ボタン状態などのM5Stackイベントを更新

  // --- ボタン操作（ローカル手動テスト用） ---
  // BtnA: LED のトグル（ON/OFF 切り替え）
  if (M5.BtnA.wasPressed()) {
    applyLedState(!ledOn);
    lastCmdLine = String("local BtnA LED toggle -> ") + (ledOn ? "ON" : "OFF");
  }
  // BtnB: テレメトリを即時送信
  if (M5.BtnB.wasPressed()) {
    publishTelemetry();
  }
  // BtnC: ハートビートを即時送信
  if (M5.BtnC.wasPressed()) {
    publishHeartbeat(true);
  }

  // --- 接続維持 ---
  ensureWiFi();
  ensureMqtt();

  // --- 定期処理（MQTT接続中のみ） ---
  if (mqttClient.connected()) {
    mqttClient.loop();  // 受信メッセージの処理（コールバック呼び出し）

    unsigned long now = millis();

    // ハートビート定期送信
    if (now - lastHeartbeatMs >= heartbeatIntervalMs) {
      publishHeartbeat(false);
    }

    // テレメトリ定期送信
    if (now - lastTelemetryMs >= telemetryIntervalMs) {
      publishTelemetry();
    }
  }

  // --- 画面更新（1秒ごと）---
  unsigned long now = millis();
  if (now - lastStatusDrawMs >= statusDrawIntervalMs) {
    lastStatusDrawMs = now;
    drawStatus();
  }

  delay(10);  // CPU占有を避けるための短いウェイト
}
