import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useWebSocketClient } from '@/hooks/use-websocket-client';
import * as auth from '@/lib/auth';

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  readyState = MockWebSocket.CONNECTING;
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: (() => void) | null = null;
  send = vi.fn();

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }
}

describe('useWebSocketClient', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.instances = [];
    vi.spyOn(auth, 'getAccessToken').mockReturnValue(null);
    vi.stubGlobal('WebSocket', MockWebSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('does not reconnect after manual disconnect', async () => {
    const { result } = renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws',
        reconnectDelayMs: 250,
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(1);
    act(() => {
      MockWebSocket.instances[0]?.open();
    });
    expect(result.current.status).toBe('open');

    act(() => {
      result.current.disconnect();
    });
    expect(result.current.status).toBe('closed');

    act(() => {
      vi.advanceTimersByTime(300);
    });

    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it('reconnects after an unexpected close and preserves the ability to resubscribe on the new socket', async () => {
    const { result } = renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws',
        reconnectDelayMs: 250,
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(1);
    act(() => {
      MockWebSocket.instances[0]?.open();
    });
    expect(result.current.status).toBe('open');

    act(() => {
      MockWebSocket.instances[0]?.close();
      vi.advanceTimersByTime(250);
    });

    expect(result.current.status).toBe('connecting');
    expect(MockWebSocket.instances).toHaveLength(2);
    act(() => {
      MockWebSocket.instances[1]?.open();
    });
    expect(result.current.status).toBe('open');

    act(() => {
      result.current.subscribe({ run_ids: ['00000000-0000-0000-0000-000000000099'] });
    });

    expect(MockWebSocket.instances[1]?.send).toHaveBeenCalledWith(
      JSON.stringify({ action: 'subscribe', run_ids: ['00000000-0000-0000-0000-000000000099'] }),
    );
  });

  it('appends access token to WebSocket URL', () => {
    vi.spyOn(auth, 'getAccessToken').mockReturnValue('test-jwt-token');

    renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws',
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0]?.url).toBe('ws://localhost:8080/ws?token=test-jwt-token');
  });

  it('connects without token param when no access token stored', () => {
    vi.spyOn(auth, 'getAccessToken').mockReturnValue(null);

    renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws',
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0]?.url).toBe('ws://localhost:8080/ws');
  });
});
