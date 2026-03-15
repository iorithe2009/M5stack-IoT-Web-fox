import { useEffect, useState, CSSProperties, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";

type TelemetryPoint = {
  ts: string;
  value: number;
  unit: string;
};

type ChartData = {
  time: string;
  value: number;
};

type Duration = "1h" | "24h";
type CommandType = "LED_SET" | "SAMPLING_INTERVAL_SET";

type CommandItem = {
  id: number;
  requestId: string;
  deviceKey?: string;
  commandType: CommandType;
  payload: Record<string, unknown>;
  status: "SENT" | "ACK" | "FAIL" | "TIMEOUT";
  errorMessage: string;
  createdAt: string;
  sentAt: string | null;
  ackAt: string | null;
  timeoutAt: string | null;
};

type WsEvent = {
  type: string;
  data: unknown;
};

function isCommandUpdatedEvent(event: WsEvent): event is { type: "command_updated"; data: CommandItem } {
  return event.type === "command_updated" && typeof event.data === "object" && event.data !== null;
}

function formatTime(isoString: string, duration: Duration): string {
  const d = new Date(isoString);
  if (duration === "24h") {
    return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}:${String(d.getSeconds()).padStart(2, "0")}`;
}

function formatDateTime(isoString: string | null): string {
  if (!isoString) return "—";
  const d = new Date(isoString);
  return `${d.getFullYear()}/${String(d.getMonth() + 1).padStart(2, "0")}/${String(d.getDate()).padStart(2, "0")} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}:${String(d.getSeconds()).padStart(2, "0")}`;
}

function toWsUrl(apiBase: string | undefined): string {
  if (!apiBase) return "";
  return apiBase.replace(/^http/, "ws") + "/ws";
}

function formatPayload(payload: Record<string, unknown>): string {
  return JSON.stringify(payload);
}

function fetchCommandHistory(
  apiBase: string | undefined,
  deviceKey: string,
  onSuccess: (items: CommandItem[]) => void,
  onError: (message: string) => void
) {
  fetch(`${apiBase}/devices/${deviceKey}/commands?limit=20`, { mode: "cors" })
    .then((r) => {
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return r.json() as Promise<CommandItem[]>;
    })
    .then((items) => {
      onSuccess(items);
      onError("");
    })
    .catch((e) => onError(String(e)));
}

export default function DeviceDetail() {
  const { deviceKey = "" } = useParams<{ deviceKey: string }>();
  const navigate = useNavigate();
  const apiBase = process.env.REACT_APP_API_SERVER;

  const [metrics, setMetrics] = useState<string[]>([]);
  const [selectedMetric, setSelectedMetric] = useState<string>("");
  const [duration, setDuration] = useState<Duration>("1h");
  const [chartData, setChartData] = useState<ChartData[]>([]);
  const [unit, setUnit] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [commands, setCommands] = useState<CommandItem[]>([]);
  const [commandError, setCommandError] = useState<string>("");
  const [submitting, setSubmitting] = useState<CommandType | "">("");
  const [samplingSeconds, setSamplingSeconds] = useState<string>("10");
  const wsRef = useRef<WebSocket | null>(null);

  // デバイス一覧APIからこのデバイスのメトリクス一覧を取得
  useEffect(() => {
    setMetrics([]);
    setSelectedMetric("");
    setChartData([]);
    setUnit("");

    fetch(`${apiBase}/devices`, { mode: "cors" })
      .then((r) => r.json())
      .then((devices: any[]) => {
        const dev = devices.find((d) => d.deviceKey === deviceKey);
        if (dev && dev.latestValues.length > 0) {
          const names: string[] = dev.latestValues.map((m: any) => m.metric);
          setMetrics(names);
          setSelectedMetric(names[0]);
        }
      })
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deviceKey]);

  // メトリクス・時間範囲が変わるたびにテレメトリを取得
  useEffect(() => {
    if (!selectedMetric) return;

    fetch(
      `${apiBase}/devices/${deviceKey}/telemetry?metric=${selectedMetric}&duration=${duration}`,
      { mode: "cors" }
    )
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json() as Promise<TelemetryPoint[]>;
      })
      .then((points) => {
        const u = points.length > 0 ? points[0].unit : "";
        setUnit(u);
        setChartData(
          points.map((p) => ({
            time: formatTime(p.ts, duration),
            value: p.value,
          }))
        );
        setError("");
      })
      .catch((e) => setError(String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedMetric, duration, deviceKey]);

  useEffect(() => {
    fetchCommandHistory(apiBase, deviceKey, setCommands, setCommandError);
  }, [apiBase, deviceKey]);

  useEffect(() => {
    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout>;
    let retryDelay = 1000;

    const connect = () => {
      const wsUrl = toWsUrl(apiBase);
      if (!wsUrl) return;

      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        retryDelay = 1000;
      };

      ws.onmessage = (ev) => {
        try {
          const event: WsEvent = JSON.parse(ev.data);
          if (!isCommandUpdatedEvent(event)) return;
          if (event.data.deviceKey !== deviceKey) return;

          setCommands((prev) => {
            const index = prev.findIndex((item) => item.requestId === event.data.requestId);
            if (index === -1) {
              fetchCommandHistory(apiBase, deviceKey, setCommands, setCommandError);
              return prev;
            }
            const next = [...prev];
            next[index] = event.data;
            return next;
          });
        } catch (_) {}
      };

      ws.onclose = () => {
        wsRef.current = null;
        if (cancelled) return;
        retryTimer = setTimeout(() => {
          retryDelay = Math.min(retryDelay * 2, 30000);
          connect();
        }, retryDelay);
      };

      ws.onerror = () => ws.close();
    };

    connect();

    return () => {
      cancelled = true;
      clearTimeout(retryTimer);
      wsRef.current?.close();
    };
  }, [apiBase, deviceKey]);

  const sendCommand = (commandType: CommandType, payload: Record<string, unknown>) => {
    setSubmitting(commandType);
    setCommandError("");

    fetch(`${apiBase}/devices/${deviceKey}/commands`, {
      method: "POST",
      mode: "cors",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ commandType, payload }),
    })
      .then(async (r) => {
        if (!r.ok) {
          throw new Error(await r.text());
        }
        return r.json() as Promise<CommandItem>;
      })
      .then((item) => {
        setCommands((prev) => [item, ...prev.filter((existing) => existing.requestId !== item.requestId)].slice(0, 20));
      })
      .catch((e) => setCommandError(String(e)))
      .finally(() => setSubmitting(""));
  };

  const btnStyle = (active: boolean): CSSProperties => ({
    padding: "4px 16px",
    border: "1px solid #888",
    borderRadius: 4,
    cursor: "pointer",
    background: active ? "#333" : "#fff",
    color: active ? "#fff" : "#333",
    fontWeight: active ? "bold" : "normal",
  });

  const tabStyle = (active: boolean): CSSProperties => ({
    padding: "6px 20px",
    border: "none",
    borderBottom: active ? "3px solid #333" : "3px solid transparent",
    cursor: "pointer",
    background: "transparent",
    fontWeight: active ? "bold" : "normal",
    fontSize: 14,
  });

  return (
    <div style={{ padding: 24, fontFamily: "sans-serif" }}>
      <button
        onClick={() => navigate("/")}
        style={{ marginBottom: 16, cursor: "pointer" }}
      >
        ← 一覧に戻る
      </button>

      <h1 style={{ marginBottom: 4 }}>{deviceKey}</h1>

      {metrics.length === 0 && (
        <p style={{ color: "#888" }}>テレメトリデータがありません。</p>
      )}

      {metrics.length > 0 && (
        <>
          {/* メトリクスタブ */}
          <div style={{ borderBottom: "1px solid #ccc", marginBottom: 16 }}>
            {metrics.map((m) => (
              <button
                key={m}
                style={tabStyle(m === selectedMetric)}
                onClick={() => setSelectedMetric(m)}
              >
                {m}
              </button>
            ))}
          </div>

          {/* 時間範囲ボタン */}
          <div style={{ display: "flex", gap: 8, marginBottom: 20 }}>
            <button style={btnStyle(duration === "1h")} onClick={() => setDuration("1h")}>
              1h
            </button>
            <button style={btnStyle(duration === "24h")} onClick={() => setDuration("24h")}>
              24h
            </button>
          </div>

          {error && <p style={{ color: "red" }}>取得エラー: {error}</p>}

          {chartData.length === 0 && !error && (
            <p style={{ color: "#888" }}>この期間のデータがありません。</p>
          )}

          {chartData.length > 0 && (
            <>
              <p style={{ margin: "0 0 8px", color: "#555" }}>
                {selectedMetric}
                {unit && ` (${unit})`} — 直近 {duration}
              </p>
              <ResponsiveContainer width="100%" height={320}>
                <LineChart data={chartData} margin={{ top: 4, right: 24, left: 0, bottom: 4 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis
                    dataKey="time"
                    tick={{ fontSize: 11 }}
                    interval="preserveStartEnd"
                  />
                  <YAxis tick={{ fontSize: 11 }} width={48} />
                  <Tooltip
                    formatter={(v: number) =>
                      [`${v}${unit ? " " + unit : ""}`, selectedMetric]
                    }
                  />
                  <Line
                    type="monotone"
                    dataKey="value"
                    dot={false}
                    stroke="#2563eb"
                    strokeWidth={2}
                  />
                </LineChart>
              </ResponsiveContainer>
            </>
          )}
        </>
      )}

      <section style={{ marginTop: 32 }}>
        <h2 style={{ margin: "0 0 12px" }}>コマンド送信</h2>
        <div style={{ display: "flex", gap: 12, flexWrap: "wrap", marginBottom: 12 }}>
          <button
            style={btnStyle(false)}
            disabled={submitting !== ""}
            onClick={() => sendCommand("LED_SET", { ledOn: true })}
          >
            LED ON
          </button>
          <button
            style={btnStyle(false)}
            disabled={submitting !== ""}
            onClick={() => sendCommand("LED_SET", { ledOn: false })}
          >
            LED OFF
          </button>
        </div>

        <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <input
            type="number"
            min={1}
            max={3600}
            value={samplingSeconds}
            onChange={(e) => setSamplingSeconds(e.target.value)}
            style={{ padding: "6px 8px", width: 120 }}
          />
          <span>秒</span>
          <button
            style={btnStyle(false)}
            disabled={submitting !== "" || !(Number.isInteger(Number(samplingSeconds)) && Number(samplingSeconds) >= 1 && Number(samplingSeconds) <= 3600)}
            onClick={() =>
              sendCommand("SAMPLING_INTERVAL_SET", {
                seconds: Number(samplingSeconds),
              })
            }
          >
            サンプリング周期を送信
          </button>
        </div>

        {commandError && (
          <p style={{ color: "red", marginTop: 12 }}>コマンドエラー: {commandError}</p>
        )}

        {submitting && (
          <p style={{ color: "#555", marginTop: 12 }}>送信中: {submitting}</p>
        )}
      </section>

      <section style={{ marginTop: 32 }}>
        <h2 style={{ margin: "0 0 12px" }}>コマンド履歴</h2>
        {commands.length === 0 && !commandError && (
          <p style={{ color: "#888" }}>履歴がありません。</p>
        )}
        {commands.length > 0 && (
          <div style={{ overflowX: "auto" }}>
            <table style={{ borderCollapse: "collapse", width: "100%" }}>
              <thead>
                <tr style={{ background: "#f0f0f0" }}>
                  <th style={th}>作成時刻</th>
                  <th style={th}>種別</th>
                  <th style={th}>payload</th>
                  <th style={th}>状態</th>
                  <th style={th}>詳細</th>
                </tr>
              </thead>
              <tbody>
                {commands.map((item) => (
                  <tr key={item.requestId} style={{ borderBottom: "1px solid #ddd" }}>
                    <td style={td}>{formatDateTime(item.createdAt)}</td>
                    <td style={td}>{item.commandType}</td>
                    <td style={td}>
                      <code>{formatPayload(item.payload)}</code>
                    </td>
                    <td style={td}>
                      <span style={statusStyle(item.status)}>{item.status}</span>
                    </td>
                    <td style={td}>
                      {item.errorMessage || `sent=${formatDateTime(item.sentAt)} ack=${formatDateTime(item.ackAt)} timeout=${formatDateTime(item.timeoutAt)}`}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

const th: CSSProperties = {
  textAlign: "left",
  padding: "8px 12px",
  borderBottom: "2px solid #ccc",
};

const td: CSSProperties = {
  padding: "10px 12px",
  verticalAlign: "top",
};

function statusStyle(status: CommandItem["status"]): CSSProperties {
  const palette: Record<CommandItem["status"], { bg: string; fg: string }> = {
    SENT: { bg: "#fff3cd", fg: "#856404" },
    ACK: { bg: "#d4edda", fg: "#155724" },
    FAIL: { bg: "#f8d7da", fg: "#721c24" },
    TIMEOUT: { bg: "#f8d7da", fg: "#721c24" },
  };

  return {
    display: "inline-block",
    padding: "2px 10px",
    borderRadius: 12,
    fontSize: 13,
    fontWeight: "bold",
    background: palette[status].bg,
    color: palette[status].fg,
  };
}
