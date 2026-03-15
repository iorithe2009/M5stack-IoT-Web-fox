import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

type LatestMetric = {
  metric: string;
  value: number;
  unit: string;
};

type Device = {
  id: number;
  deviceKey: string;
  name: string;
  online: boolean;
  lastSeenAt: string | null;
  latestValues: LatestMetric[];
};

type WsEvent = {
  type: string;
  data: {
    deviceKey: string;
    online?: boolean;
    lastSeenAt?: string | null;
  };
};

function timeAgo(isoString: string | null): string {
  if (!isoString) return "—";
  const diff = Math.floor((Date.now() - new Date(isoString).getTime()) / 1000);
  if (diff < 60) return `${diff}秒前`;
  if (diff < 3600) return `${Math.floor(diff / 60)}分前`;
  return `${Math.floor(diff / 3600)}時間前`;
}

function toWsUrl(apiBase: string | undefined): string {
  if (!apiBase) return "";
  return apiBase.replace(/^http/, "ws") + "/ws";
}

export default function DeviceList() {
  const [devices, setDevices] = useState<Device[]>([]);
  const [error, setError] = useState<string>("");
  const navigate = useNavigate();
  const apiBase = process.env.REACT_APP_API_SERVER;
  const retryDelay = useRef(1000);
  const wsRef = useRef<WebSocket | null>(null);

  const fetchDevices = () => {
    return fetch(`${apiBase}/devices`, { mode: "cors" })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json() as Promise<Device[]>;
      })
      .then((data) => {
        setDevices(data);
        setError("");
      })
      .catch((e) => setError(String(e)));
  };

  useEffect(() => {
    fetchDevices();

    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout>;

    const connect = () => {
      const wsUrl = toWsUrl(apiBase);
      if (!wsUrl) return;

      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        retryDelay.current = 1000;
        // 再接続時は REST で全件リセット
        fetchDevices();
      };

      ws.onmessage = (ev) => {
        try {
          const event: WsEvent = JSON.parse(ev.data);
          if (event.type === "device_state_changed") {
            const { deviceKey, online, lastSeenAt } = event.data;
            setDevices((prev) => {
              const found = prev.some((d) => d.deviceKey === deviceKey);
              if (!found) {
                fetchDevices();
                return prev;
              }
              return prev.map((d) =>
                d.deviceKey === deviceKey
                  ? {
                      ...d,
                      online: online ?? d.online,
                      lastSeenAt: lastSeenAt !== undefined ? lastSeenAt : d.lastSeenAt,
                    }
                  : d
              );
            });
          }
        } catch (_) {}
      };

      ws.onclose = () => {
        wsRef.current = null;
        if (cancelled) return;
        retryTimer = setTimeout(() => {
          retryDelay.current = Math.min(retryDelay.current * 2, 30000);
          connect();
        }, retryDelay.current);
      };

      ws.onerror = () => ws.close();
    };

    connect();

    return () => {
      cancelled = true;
      clearTimeout(retryTimer);
      wsRef.current?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div style={{ padding: 24, fontFamily: "sans-serif" }}>
      <h1 style={{ marginBottom: 4 }}>IoT デバイス一覧</h1>
      {error && (
        <p style={{ color: "red" }}>取得エラー: {error}</p>
      )}
      {devices.length === 0 && !error && (
        <p style={{ color: "#888" }}>デバイスがまだ登録されていません。</p>
      )}
      {devices.length > 0 && (
        <table style={{ borderCollapse: "collapse", width: "100%" }}>
          <thead>
            <tr style={{ background: "#f0f0f0" }}>
              <th style={th}>デバイス</th>
              <th style={th}>状態</th>
              <th style={th}>最新値</th>
              <th style={th}>最終受信</th>
            </tr>
          </thead>
          <tbody>
            {devices.map((dev) => (
              <tr
                key={dev.id}
                onClick={() => navigate(`/devices/${dev.deviceKey}`)}
                style={{ cursor: "pointer", borderBottom: "1px solid #ddd" }}
                onMouseEnter={(e) =>
                  (e.currentTarget.style.background = "#f9f9f9")
                }
                onMouseLeave={(e) =>
                  (e.currentTarget.style.background = "transparent")
                }
              >
                <td style={td}>
                  <strong>{dev.name || dev.deviceKey}</strong>
                  {dev.name && (
                    <span style={{ color: "#888", marginLeft: 6, fontSize: 12 }}>
                      ({dev.deviceKey})
                    </span>
                  )}
                </td>
                <td style={td}>
                  <span
                    style={{
                      display: "inline-block",
                      padding: "2px 10px",
                      borderRadius: 12,
                      fontSize: 13,
                      fontWeight: "bold",
                      background: dev.online ? "#d4edda" : "#f8d7da",
                      color: dev.online ? "#155724" : "#721c24",
                    }}
                  >
                    {dev.online ? "Online" : "Offline"}
                  </span>
                </td>
                <td style={td}>
                  {dev.latestValues.length === 0 ? (
                    <span style={{ color: "#aaa" }}>—</span>
                  ) : (
                    dev.latestValues.map((m) => (
                      <span key={m.metric} style={{ marginRight: 12 }}>
                        {m.metric}: <strong>{m.value}</strong>
                        {m.unit && <span style={{ color: "#666" }}> {m.unit}</span>}
                      </span>
                    ))
                  )}
                </td>
                <td style={{ ...td, color: "#666", fontSize: 13 }}>
                  {timeAgo(dev.lastSeenAt)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

const th: React.CSSProperties = {
  textAlign: "left",
  padding: "8px 12px",
  borderBottom: "2px solid #ccc",
};

const td: React.CSSProperties = {
  padding: "10px 12px",
  verticalAlign: "middle",
};
