import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Radio, Wifi, WifiOff } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { useWebSocketClient } from '@/hooks/use-websocket-client';
import { apiClient } from '@/lib/api/client';
import type { AgentEvent, WebSocketMessage, WebSocketServerMessage } from '@/lib/api/types';

const MAX_FEED_ITEMS = 50;

interface FeedItem {
  id: string;
  type: string;
  title: string;
  detail?: string;
  strategyId?: string;
  runId?: string;
  timestamp: string;
}

function eventLabel(type: string): string {
  const labels: Record<string, string> = {
    pipeline_start: 'Pipeline started',
    pipeline_started: 'Pipeline started',
    pipeline_completed: 'Pipeline completed',
    pipeline_failed: 'Pipeline failed',
    agent_started: 'Agent started',
    agent_completed: 'Agent completed',
    agent_decision: 'Agent decision',
    debate_round: 'Debate round',
    debate_round_completed: 'Debate round',
    signal: 'Signal',
    signal_produced: 'Signal produced',
    order_submitted: 'Order submitted',
    order_filled: 'Order filled',
    position_update: 'Position update',
    circuit_breaker: 'Circuit breaker',
    error: 'Error',
    pipeline_health: 'Pipeline health',
    phase_started: 'Phase started',
    phase_completed: 'Phase completed',
  };
  return labels[type] ?? type.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase());
}

function eventVariant(type: string): 'default' | 'secondary' | 'destructive' | 'success' | 'warning' {
  switch (type) {
    case 'signal':
    case 'signal_produced':
    case 'order_filled':
    case 'pipeline_completed':
      return 'success';
    case 'circuit_breaker':
    case 'error':
    case 'pipeline_failed':
      return 'destructive';
    case 'order_submitted':
    case 'position_update':
    case 'pipeline_health':
      return 'warning';
    default:
      return 'secondary';
  }
}

function summarizeLiveData(data: unknown) {
  if (typeof data === 'string') {
    return data;
  }

  if (data && typeof data === 'object') {
    if ('summary' in data && typeof data.summary === 'string') {
      return data.summary;
    }
    if ('signal' in data && typeof data.signal === 'string') {
      return `Signal ${String(data.signal).toUpperCase()}`;
    }
    if ('ticker' in data && typeof data.ticker === 'string') {
      return `Ticker ${data.ticker}`;
    }
  }

  return undefined;
}

function toHistoricalFeedItem(event: AgentEvent): FeedItem {
  return {
    id: event.id,
    type: event.event_kind,
    title: event.title || eventLabel(event.event_kind),
    detail: event.summary,
    strategyId: event.strategy_id,
    runId: event.pipeline_run_id,
    timestamp: event.created_at,
  };
}

function toLiveFeedItem(msg: WebSocketMessage): FeedItem {
  return {
    id: `${msg.type}-${msg.run_id ?? 'none'}-${msg.timestamp ?? Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    type: msg.type,
    title: eventLabel(msg.type),
    detail: summarizeLiveData(msg.data),
    strategyId: msg.strategy_id,
    runId: msg.run_id,
    timestamp: msg.timestamp ?? new Date().toISOString(),
  };
}

function isWebSocketMessage(msg: WebSocketServerMessage): msg is WebSocketMessage {
  return 'type' in msg && !('status' in msg);
}

export function ActivityFeed() {
  const [liveItems, setLiveItems] = useState<FeedItem[]>([]);
  const subscribedRef = useRef(false);
  const { data } = useQuery({
    queryKey: ['events', 'dashboard-activity-feed'],
    queryFn: () => apiClient.listEvents({ limit: 20 }),
    refetchInterval: 30_000,
  });

  const handleMessage = useCallback((msg: WebSocketServerMessage) => {
    if (!isWebSocketMessage(msg)) return;

    const item = toLiveFeedItem(msg);

    setLiveItems((prev) => {
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

  const items = useMemo(() => {
    const historicalItems = (data?.data ?? []).map(toHistoricalFeedItem);
    const merged = [...liveItems, ...historicalItems];
    const byId = new Map<string, FeedItem>();
    for (const item of merged) {
      if (!byId.has(item.id)) {
        byId.set(item.id, item);
      }
    }

    return Array.from(byId.values())
      .sort((left, right) => new Date(right.timestamp).getTime() - new Date(left.timestamp).getTime())
      .slice(0, MAX_FEED_ITEMS);
  }, [data?.data, liveItems]);

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
                className="flex items-start gap-3 rounded-lg border border-border p-3 text-sm transition-colors hover:bg-accent/45"
              >
                <Badge variant={eventVariant(item.type)} className="mt-0.5 shrink-0">
                  {eventLabel(item.type)}
                </Badge>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium text-foreground">{item.title}</p>
                  {item.detail ? <p className="mt-1 text-sm text-muted-foreground">{item.detail}</p> : null}
                  <div className="mt-2 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                    {item.strategyId ? (
                      <span className="mr-2">Strategy: {item.strategyId.slice(0, 8)}…</span>
                    ) : null}
                    {item.runId ? <span>Run: {item.runId}</span> : null}
                  </div>
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
