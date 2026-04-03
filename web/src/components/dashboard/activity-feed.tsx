import { useCallback, useEffect, useRef, useState } from 'react';
import { Radio, Wifi, WifiOff } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { useWebSocketClient } from '@/hooks/use-websocket-client';
import type { WebSocketEventType, WebSocketMessage, WebSocketServerMessage } from '@/lib/api/types';

const MAX_FEED_ITEMS = 50;

interface FeedItem {
  id: string;
  type: WebSocketEventType;
  strategyId?: string;
  runId?: string;
  timestamp: string;
  summary: string;
}

function eventLabel(type: WebSocketEventType): string {
  const labels: Record<WebSocketEventType, string> = {
    pipeline_start: 'Pipeline started',
    agent_decision: 'Agent decision',
    debate_round: 'Debate round',
    signal: 'Signal',
    order_submitted: 'Order submitted',
    order_filled: 'Order filled',
    position_update: 'Position update',
    circuit_breaker: 'Circuit breaker',
    error: 'Error',
  };
  return labels[type] ?? type;
}

function eventVariant(
  type: WebSocketEventType,
): 'default' | 'secondary' | 'destructive' | 'success' | 'warning' {
  switch (type) {
    case 'signal':
    case 'order_filled':
      return 'success';
    case 'circuit_breaker':
    case 'error':
      return 'destructive';
    case 'order_submitted':
    case 'position_update':
      return 'warning';
    default:
      return 'secondary';
  }
}

function toFeedItem(msg: WebSocketMessage): FeedItem {
  return {
    id: `${msg.type}-${msg.timestamp ?? Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    type: msg.type,
    strategyId: msg.strategy_id,
    runId: msg.run_id,
    timestamp: msg.timestamp ?? new Date().toISOString(),
    summary: eventLabel(msg.type),
  };
}

function isWebSocketMessage(msg: WebSocketServerMessage): msg is WebSocketMessage {
  return 'type' in msg && !('status' in msg);
}

export function ActivityFeed() {
  const [items, setItems] = useState<FeedItem[]>([]);
  const subscribedRef = useRef(false);

  const handleMessage = useCallback((msg: WebSocketServerMessage) => {
    if (!isWebSocketMessage(msg)) return;

    const item = toFeedItem(msg);

    setItems((prev) => {
      const next = [item, ...prev];
      return next.length > MAX_FEED_ITEMS ? next.slice(0, MAX_FEED_ITEMS) : next;
    });
  }, []);

  const { status, subscribeAll } = useWebSocketClient({
    enabled: true,
    onMessage: handleMessage,
  });

  const isConnected = status === 'open';

  useEffect(() => {
    if (isConnected && !subscribedRef.current) {
      subscribeAll();
      subscribedRef.current = true;
    }
    if (!isConnected) {
      subscribedRef.current = false;
    }
  }, [isConnected, subscribeAll]);

  return (
    <Card data-testid="activity-feed">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>Live activity</CardTitle>
            <CardDescription>Real-time pipeline and trading events</CardDescription>
          </div>
          <Badge variant={isConnected ? 'success' : 'outline'} className="gap-1">
            {isConnected ? <Wifi className="size-3" /> : <WifiOff className="size-3" />}
            {isConnected ? 'Connected' : status}
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        {items.length === 0 ? (
          <div
            className="flex flex-col items-center gap-2 py-8 text-center"
            data-testid="activity-feed-empty"
          >
            <Radio className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              {isConnected ? 'Listening for events…' : 'Waiting for WebSocket connection'}
            </p>
          </div>
        ) : (
          <ul className="space-y-2" data-testid="activity-feed-list">
            {items.map((item) => (
              <li
                key={item.id}
                className="flex items-start gap-3 rounded-lg border border-white/8 bg-background/45 p-3 text-sm transition-colors hover:border-primary/15 hover:bg-accent/45"
              >
                <Badge variant={eventVariant(item.type)} className="mt-0.5 shrink-0">
                  {item.summary}
                </Badge>
                <div className="min-w-0 flex-1 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                  {item.strategyId ? (
                    <span className="mr-2">Strategy: {item.strategyId.slice(0, 8)}…</span>
                  ) : null}
                  {item.runId ? <span>Run: {item.runId.slice(0, 8)}…</span> : null}
                </div>
                <time
                  className="shrink-0 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground"
                  dateTime={new Date(item.timestamp).toISOString()}
                >
                  {new Date(item.timestamp).toLocaleTimeString()}
                </time>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
