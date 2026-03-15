import React from "react";
import { render, screen, waitFor } from "@testing-library/react";
import App from "./App";

const fetchMock = jest.fn();

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(public url: string) {
    MockWebSocket.instances.push(this);
    setTimeout(() => this.onopen?.(), 0);
  }

  close() {
    this.onclose?.();
  }
}

describe("App", () => {
  beforeEach(() => {
    process.env.REACT_APP_API_SERVER = "http://localhost:8080/api";
    fetchMock.mockReset();
    MockWebSocket.instances = [];
    window.history.pushState({}, "", "/");
    global.fetch = fetchMock as unknown as typeof fetch;
    (global as typeof globalThis & { WebSocket: typeof WebSocket }).WebSocket =
      MockWebSocket as unknown as typeof WebSocket;
  });

  it("renders device list fetched from the API", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => [
        {
          id: 1,
          deviceKey: "m5-001",
          name: "M5 Fire",
          online: true,
          lastSeenAt: "2026-03-15T12:00:00Z",
          latestValues: [{ metric: "temp", value: 26.3, unit: "C" }],
        },
      ],
    });

    render(<App />);

    expect(await screen.findByText("IoT デバイス一覧")).toBeInTheDocument();
    expect(await screen.findByText("M5 Fire")).toBeInTheDocument();
    expect(screen.getByText("Online")).toBeInTheDocument();
    expect(screen.getByText(/temp:/)).toBeInTheDocument();

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://localhost:8080/api/devices",
        { mode: "cors" }
      );
    });
  });
});
