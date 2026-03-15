import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

describe("App e2e smoke", () => {
  beforeEach(() => {
    process.env.REACT_APP_API_SERVER = "http://localhost:8080/api";
    fetchMock.mockReset();
    MockWebSocket.instances = [];
    window.history.pushState({}, "", "/");
    global.fetch = fetchMock as unknown as typeof fetch;
    (global as typeof globalThis & { WebSocket: typeof WebSocket }).WebSocket =
      MockWebSocket as unknown as typeof WebSocket;
  });

  it("navigates from device list to detail and sends an LED command", async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url === "http://localhost:8080/api/devices") {
        return Promise.resolve({
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
      }

      if (
        url ===
        "http://localhost:8080/api/devices/m5-001/telemetry?metric=temp&duration=1h"
      ) {
        return Promise.resolve({
          ok: true,
          json: async () => [
            { ts: "2026-03-15T12:00:00Z", value: 26.3, unit: "C" },
          ],
        });
      }

      if (url === "http://localhost:8080/api/devices/m5-001/commands?limit=20") {
        return Promise.resolve({
          ok: true,
          json: async () => [],
        });
      }

      if (
        url === "http://localhost:8080/api/devices/m5-001/commands" &&
        init?.method === "POST"
      ) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: 1,
            requestId: "cmd_test_001",
            deviceKey: "m5-001",
            commandType: "LED_SET",
            payload: { ledOn: true },
            status: "SENT",
            errorMessage: "",
            createdAt: "2026-03-15T12:00:00Z",
            sentAt: "2026-03-15T12:00:00Z",
            ackAt: null,
            timeoutAt: null,
          }),
        });
      }

      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    render(<App />);

    fireEvent.click(await screen.findByText("M5 Fire"));

    expect(await screen.findByText("m5-001")).toBeInTheDocument();
    expect(await screen.findByText("コマンド送信")).toBeInTheDocument();
    expect(await screen.findByText("LED ON")).toBeInTheDocument();

    fireEvent.click(screen.getByText("LED ON"));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://localhost:8080/api/devices/m5-001/commands",
        expect.objectContaining({
          method: "POST",
          headers: { "Content-Type": "application/json" },
        })
      );
    });

    expect(await screen.findByText("SENT")).toBeInTheDocument();
  });
});
