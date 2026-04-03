import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Activity,
  AlertCircle,
  ArrowDown,
  Brain,
  CandlestickChart,
  CircleDot,
  FileWarning,
  Layers3,
  ShieldAlert,
  ShoppingBag,
} from 'lucide-react';
import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { ChatPanel, type ChatMessage } from '@/components/chat/chat-panel';
import { PageHeader } from '@/components/layout/page-header';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { useWebSocketClient } from '@/hooks/use-websocket-client';
import { AGENT_ROLE_OPTIONS, formatAgentRole } from '@/lib/agent-roles';
import { apiClient } from '@/lib/api/client';
import type {
  AgentEvent,
  AgentRole,
  Conversation,
  ConversationMessage,
  ListResponse,
  PipelineRun,
  WebSocketMessage,
  WebSocketServerMessage,
} from '@/lib/api/types';
import { cn } from '@/lib/utils';

const MAX_LIVE_EVENTS = 100;
const CONVERSATION_PAGE_SIZE = 50;
const MESSAGE_PAGE_SIZE = 100;
const RUN_PAGE_SIZE = 50;
const NEW_CONVERSATION_VALUE = '__new__';
const denseSelectClassName =
  'flex h-9 w-full rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring';

type FeedItem = AgentEvent & { live?: boolean };
type ChatSelectionSource = 'event' | 'manual' | 'creating';
type ChatContext = { pipelineRunId: string; agentRole: AgentRole };

function isWebSocketMessage(message: WebSocketServerMessage): message is WebSocketMessage {
  return 'type' in message && !('error' in message) && !('status' in message);
}

function eventIcon(kind: string) {
  const icons: Record<string, typeof Activity> = {
    pipeline_start: Layers3,
    pipeline_started: Layers3,
    phase_started: Layers3,
    agent_started: Brain,
    agent_completed: Brain,
    agent_decision: Brain,
    debate_round: CandlestickChart,
    debate_round_completed: CandlestickChart,
    signal: CircleDot,
    signal_emitted: CircleDot,
    order_submitted: ShoppingBag,
    order_filled: ShoppingBag,
    position_update: CandlestickChart,
    circuit_breaker: ShieldAlert,
    error: AlertCircle,
  };

  return icons[kind] ?? FileWarning;
}

function eventVariant(
  kind: string,
): 'default' | 'secondary' | 'destructive' | 'success' | 'warning' {
  switch (kind) {
    case 'signal':
    case 'signal_emitted':
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

function eventLabel(kind: string) {
  return kind.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase());
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

function toFeedItem(message: WebSocketMessage): FeedItem {
  const now = message.timestamp ?? new Date().toISOString();
  const data = message.data as Record<string, unknown> | undefined;

  return {
    id: `${message.type}-${message.run_id ?? 'none'}-${now}-${Math.random().toString(36).slice(2, 8)}`,
    pipeline_run_id: message.run_id,
    strategy_id: message.strategy_id,
    agent_role:
      typeof data?.agent_role === 'string'
        ? (data.agent_role as AgentEvent['agent_role'])
        : undefined,
    event_kind: message.type,
    title: eventLabel(message.type),
    summary: summarizeLiveData(message.data),
    metadata: message.data,
    created_at: now,
    live: true,
  };
}

function sortEvents(events: FeedItem[]) {
  return [...events].sort(
    (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
  );
}

function sortConversations(conversations: Conversation[]) {
  return [...conversations].sort(
    (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
  );
}

function relativeTime(value: string) {
  const deltaSeconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
  if (deltaSeconds < 60) return `${deltaSeconds}s ago`;
  const minutes = Math.floor(deltaSeconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function toChatMessage(
  message: ConversationMessage,
  agentRole: AgentRole | undefined,
): ChatMessage {
  return {
    id: message.id,
    role: message.role,
    content: message.content,
    agent_role: message.role === 'assistant' ? agentRole : undefined,
    created_at: message.created_at,
  };
}

function toChatContext(
  pipelineRunId: string | undefined,
  agentRole: AgentEvent['agent_role'] | Conversation['agent_role'] | undefined,
): ChatContext | null {
  if (!pipelineRunId || !agentRole) {
    return null;
  }

  return { pipelineRunId, agentRole };
}

function sameChatContext(left: ChatContext | null, right: ChatContext | null) {
  if (!left || !right) {
    return left === right;
  }

  return left.pipelineRunId === right.pipelineRunId && left.agentRole === right.agentRole;
}

function findConversationForContext(conversations: Conversation[], context: ChatContext | null) {
  if (!context) {
    return null;
  }

  return (
    conversations.find(
      (conversation) =>
        conversation.pipeline_run_id === context.pipelineRunId &&
        conversation.agent_role === context.agentRole,
    ) ?? null
  );
}

function parseConversationTicker(title?: string) {
  if (!title) {
    return null;
  }

  const [, ticker] = title.split('—');
  return ticker?.trim() || null;
}

function formatConversationLabel(conversation: Conversation) {
  const ticker = parseConversationTicker(conversation.title) ?? conversation.pipeline_run_id;
  return `${formatAgentRole(conversation.agent_role)} · ${ticker} · ${new Date(conversation.updated_at).toLocaleString()}`;
}

function formatRunLabel(run: PipelineRun) {
  return `${run.ticker} · ${run.id} · ${new Date(run.started_at).toLocaleString()}`;
}

function mergeConversation(
  createdConversation: Conversation,
  current?: ListResponse<Conversation>,
) {
  const existing = current?.data ?? [];

  return {
    ...current,
    data: sortConversations([
      createdConversation,
      ...existing.filter((conversation) => conversation.id !== createdConversation.id),
    ]),
    limit: current?.limit ?? CONVERSATION_PAGE_SIZE,
    offset: current?.offset ?? 0,
  };
}

function buildEventContextMessage(event: FeedItem, conversation?: Conversation) {
  const agentLabel = event.agent_role ? formatAgentRole(event.agent_role) : 'agent';
  const ticker =
    parseConversationTicker(conversation?.title) ?? event.pipeline_run_id ?? 'this run';

  return `Context note (UI only, not saved to the conversation): discussing ${agentLabel} event "${event.title}" for ${ticker}.`;
}

export function RealtimePage() {
  const queryClient = useQueryClient();
  const [liveEvents, setLiveEvents] = useState<FeedItem[]>([]);
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null);
  const [userSelectedEventId, setUserSelectedEventId] = useState<string | null>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [chatSelectionSource, setChatSelectionSource] = useState<ChatSelectionSource>('event');
  const [chatError, setChatError] = useState<string | null>(null);
  const [isSendingMessage, setIsSendingMessage] = useState(false);
  const [isCreatingConversation, setIsCreatingConversation] = useState(false);
  const [isResolvingEventConversation, setIsResolvingEventConversation] = useState(false);
  const [createConversationDraft, setCreateConversationDraft] = useState<{
    pipelineRunId: string;
    agentRole: AgentRole | '';
  }>({ pipelineRunId: '', agentRole: '' });
  const subscribedRef = useRef(false);
  const autoCreatedEventConversationIdsRef = useRef(new Set<string>());
  const eventConversationRequestRef = useRef(0);
  const feedRef = useRef<HTMLDivElement>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['events', 'realtime-page'],
    queryFn: () => apiClient.listEvents({ limit: 50 }),
  });

  const handleMessage = useCallback((message: WebSocketServerMessage) => {
    if (!isWebSocketMessage(message)) {
      return;
    }

    setLiveEvents((prev) => {
      const next = [...prev, toFeedItem(message)];
      return next.length > MAX_LIVE_EVENTS ? next.slice(next.length - MAX_LIVE_EVENTS) : next;
    });
  }, []);

  const { status, subscribeAll } = useWebSocketClient({
    enabled: true,
    onMessage: handleMessage,
  });

  useEffect(() => {
    if (status === 'open' && !subscribedRef.current) {
      subscribeAll();
      subscribedRef.current = true;
    }

    if (status !== 'open') {
      subscribedRef.current = false;
    }
  }, [status, subscribeAll]);

  const events = useMemo(() => {
    const historical = (data?.data ?? []).map((event) => ({ ...event, live: false as const }));
    const merged = [...historical, ...liveEvents];
    const byId = new Map<string, FeedItem>();
    for (const event of merged) {
      byId.set(event.id, event);
    }
    return sortEvents(Array.from(byId.values()));
  }, [data?.data, liveEvents]);

  const selectedEvent = useMemo(
    () => events.find((event) => event.id === selectedEventId) ?? null,
    [events, selectedEventId],
  );

  const eventChatContext = useMemo(
    () => toChatContext(selectedEvent?.pipeline_run_id, selectedEvent?.agent_role),
    [selectedEvent?.agent_role, selectedEvent?.pipeline_run_id],
  );

  const conversationQueryKey = useMemo(() => ['conversations', 'realtime-page'] as const, []);

  const {
    data: conversationsData,
    isLoading: isConversationLoading,
    isError: isConversationError,
    error: conversationError,
  } = useQuery({
    queryKey: conversationQueryKey,
    queryFn: () => apiClient.listConversations({ limit: CONVERSATION_PAGE_SIZE }),
  });

  const conversations = useMemo(
    () => sortConversations(conversationsData?.data ?? []),
    [conversationsData?.data],
  );

  const selectedConversation = useMemo(
    () => conversations.find((conversation) => conversation.id === conversationId) ?? null,
    [conversationId, conversations],
  );

  useEffect(() => {
    if (!selectedEventId && events.length > 0) {
      setSelectedEventId(events[events.length - 1]?.id ?? null);
    }
  }, [events, selectedEventId]);

  useEffect(() => {
    setChatSelectionSource('event');
    setConversationId(null);
    setChatError(null);
  }, [selectedEventId]);

  useEffect(() => {
    if (chatSelectionSource !== 'event') {
      eventConversationRequestRef.current += 1;
      setIsResolvingEventConversation(false);
      return;
    }

    if (!eventChatContext) {
      eventConversationRequestRef.current += 1;
      setConversationId(null);
      setIsResolvingEventConversation(false);
      return;
    }

    const existingConversation = findConversationForContext(conversations, eventChatContext);
    if (existingConversation) {
      setConversationId(existingConversation.id);
      setIsResolvingEventConversation(false);
      return;
    }

    if (selectedEventId !== userSelectedEventId) {
      setConversationId(null);
      setIsResolvingEventConversation(false);
      return;
    }

    const requestId = eventConversationRequestRef.current + 1;
    eventConversationRequestRef.current = requestId;
    setConversationId(null);
    setIsResolvingEventConversation(true);

    void (async () => {
      try {
        const createdConversation = await apiClient.createConversation({
          pipeline_run_id: eventChatContext.pipelineRunId,
          agent_role: eventChatContext.agentRole,
        });

        autoCreatedEventConversationIdsRef.current.add(createdConversation.id);
        queryClient.setQueryData(conversationQueryKey, (current?: ListResponse<Conversation>) =>
          mergeConversation(createdConversation, current),
        );
        queryClient.setQueryData(
          ['conversation-messages', 'realtime-page', createdConversation.id],
          {
            data: [],
            limit: MESSAGE_PAGE_SIZE,
            offset: 0,
          },
        );

        if (eventConversationRequestRef.current === requestId) {
          setConversationId(createdConversation.id);
        }
      } catch (error) {
        if (eventConversationRequestRef.current === requestId) {
          setChatError(getErrorMessage(error, 'Unable to create conversation.'));
        }
      } finally {
        if (eventConversationRequestRef.current === requestId) {
          setIsResolvingEventConversation(false);
        }
      }
    })();
  }, [
    chatSelectionSource,
    conversationQueryKey,
    conversations,
    eventChatContext,
    queryClient,
    selectedEventId,
    userSelectedEventId,
  ]);

  const activeChatContext = useMemo(
    () =>
      toChatContext(selectedConversation?.pipeline_run_id, selectedConversation?.agent_role) ??
      eventChatContext,
    [eventChatContext, selectedConversation?.agent_role, selectedConversation?.pipeline_run_id],
  );

  const messageQueryKey = useMemo(
    () => ['conversation-messages', 'realtime-page', conversationId] as const,
    [conversationId],
  );

  const {
    data: messagesData,
    isLoading: isMessagesLoading,
    isError: isMessagesError,
    error: messagesError,
  } = useQuery({
    queryKey: messageQueryKey,
    queryFn: () => apiClient.getConversationMessages(conversationId!, { limit: MESSAGE_PAGE_SIZE }),
    enabled: conversationId !== null,
    staleTime: 30_000,
  });

  const chatMessages = useMemo(
    () =>
      (messagesData?.data ?? []).map((message) =>
        toChatMessage(message, activeChatContext?.agentRole),
      ),
    [activeChatContext?.agentRole, messagesData?.data],
  );

  const eventContextMessage = useMemo(() => {
    if (
      chatSelectionSource !== 'event' ||
      !selectedConversation ||
      !selectedEvent ||
      !eventChatContext ||
      !sameChatContext(activeChatContext, eventChatContext) ||
      !autoCreatedEventConversationIdsRef.current.has(selectedConversation.id)
    ) {
      return null;
    }

    return {
      id: `context-${selectedConversation.id}`,
      role: 'system' as const,
      content: buildEventContextMessage(selectedEvent, selectedConversation),
      created_at: selectedConversation.created_at,
    };
  }, [
    activeChatContext,
    chatSelectionSource,
    eventChatContext,
    selectedConversation,
    selectedEvent,
  ]);

  const visibleChatMessages = useMemo(
    () => (eventContextMessage ? [eventContextMessage, ...chatMessages] : chatMessages),
    [chatMessages, eventContextMessage],
  );

  const {
    data: runsData,
    isLoading: isRunsLoading,
    isError: isRunsError,
    error: runsError,
  } = useQuery({
    queryKey: ['runs', 'realtime-page', 'new-conversation'],
    queryFn: () => apiClient.listRuns({ limit: RUN_PAGE_SIZE }),
    enabled: chatSelectionSource === 'creating',
  });

  const runOptions = useMemo(() => {
    const options = (runsData?.data ?? []).map((run) => ({
      id: run.id,
      label: formatRunLabel(run),
    }));

    if (
      selectedEvent?.pipeline_run_id &&
      !options.some((option) => option.id === selectedEvent.pipeline_run_id)
    ) {
      options.unshift({ id: selectedEvent.pipeline_run_id, label: selectedEvent.pipeline_run_id });
    }

    return options;
  }, [runsData?.data, selectedEvent?.pipeline_run_id]);

  useEffect(() => {
    if (chatSelectionSource !== 'creating') {
      return;
    }

    if (!createConversationDraft.pipelineRunId && runOptions[0]?.id) {
      setCreateConversationDraft((current) => ({
        ...current,
        pipelineRunId: runOptions[0]?.id ?? '',
      }));
    }
  }, [chatSelectionSource, createConversationDraft.pipelineRunId, runOptions]);

  const refreshConversationMessages = useCallback(
    async (id: string) => {
      const latest = await apiClient.getConversationMessages(id, { limit: MESSAGE_PAGE_SIZE });
      queryClient.setQueryData(['conversation-messages', 'realtime-page', id], latest);
      return latest;
    },
    [queryClient],
  );

  const handleSendMessage = useCallback(
    async (content: string) => {
      if (!activeChatContext || isSendingMessage) {
        return;
      }

      setIsSendingMessage(true);
      setChatError(null);

      let activeConversationId = conversationId;

      try {
        if (!activeConversationId) {
          const createdConversation = await apiClient.createConversation({
            pipeline_run_id: activeChatContext.pipelineRunId,
            agent_role: activeChatContext.agentRole,
          });
          activeConversationId = createdConversation.id;
          setConversationId(activeConversationId);
          queryClient.setQueryData(conversationQueryKey, (current?: ListResponse<Conversation>) =>
            mergeConversation(createdConversation, current),
          );
        }

        await apiClient.createConversationMessage(activeConversationId, { content });
        await refreshConversationMessages(activeConversationId);
      } catch (error) {
        if (activeConversationId) {
          try {
            await refreshConversationMessages(activeConversationId);
          } catch {
            // Keep the original send error. Refresh is best effort only.
          }
        }

        setChatError(getErrorMessage(error, 'Unable to send message.'));
      } finally {
        setIsSendingMessage(false);
      }
    },
    [
      activeChatContext,
      conversationId,
      conversationQueryKey,
      isSendingMessage,
      queryClient,
      refreshConversationMessages,
    ],
  );

  const openCreateConversationForm = useCallback(() => {
    setChatSelectionSource('creating');
    setChatError(null);
    setCreateConversationDraft({
      pipelineRunId: selectedConversation?.pipeline_run_id ?? eventChatContext?.pipelineRunId ?? '',
      agentRole: selectedConversation?.agent_role ?? eventChatContext?.agentRole ?? '',
    });
  }, [eventChatContext, selectedConversation?.agent_role, selectedConversation?.pipeline_run_id]);

  const handleConversationSelection = useCallback(
    (value: string) => {
      if (value === NEW_CONVERSATION_VALUE) {
        openCreateConversationForm();
        return;
      }

      setChatError(null);

      if (!value) {
        setChatSelectionSource('event');
        setConversationId(null);
        return;
      }

      setChatSelectionSource('manual');
      setConversationId(value);
    },
    [openCreateConversationForm],
  );

  const handleCreateConversation = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();

      if (
        !createConversationDraft.pipelineRunId ||
        !createConversationDraft.agentRole ||
        isCreatingConversation
      ) {
        return;
      }

      setIsCreatingConversation(true);
      setChatError(null);

      try {
        const createdConversation = await apiClient.createConversation({
          pipeline_run_id: createConversationDraft.pipelineRunId,
          agent_role: createConversationDraft.agentRole,
        });

        queryClient.setQueryData(conversationQueryKey, (current?: ListResponse<Conversation>) =>
          mergeConversation(createdConversation, current),
        );
        queryClient.setQueryData(
          ['conversation-messages', 'realtime-page', createdConversation.id],
          {
            data: [],
            limit: MESSAGE_PAGE_SIZE,
            offset: 0,
          },
        );
        setConversationId(createdConversation.id);
        setChatSelectionSource('manual');
      } catch (error) {
        setChatError(getErrorMessage(error, 'Unable to create conversation.'));
      } finally {
        setIsCreatingConversation(false);
      }
    },
    [
      conversationQueryKey,
      createConversationDraft.agentRole,
      createConversationDraft.pipelineRunId,
      isCreatingConversation,
      queryClient,
    ],
  );

  const selectedConversationOutsideEventContext =
    !!selectedConversation &&
    !!eventChatContext &&
    !sameChatContext(activeChatContext, eventChatContext);

  const resolvedChatError =
    chatError ??
    (isConversationError
      ? getErrorMessage(conversationError, 'Unable to load conversations.')
      : chatSelectionSource === 'creating' && isRunsError
        ? getErrorMessage(runsError, 'Unable to load pipeline runs.')
        : isMessagesError
          ? getErrorMessage(messagesError, 'Unable to load conversation messages.')
          : null);

  useEffect(() => {
    if (!autoScroll || !feedRef.current) {
      return;
    }

    feedRef.current.scrollTop = feedRef.current.scrollHeight;
  }, [autoScroll, events]);

  function handleFeedScroll() {
    const element = feedRef.current;
    if (!element) {
      return;
    }

    const nearBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
    setAutoScroll(nearBottom);
  }

  function resumeScroll() {
    setAutoScroll(true);
    if (feedRef.current) {
      feedRef.current.scrollTop = feedRef.current.scrollHeight;
    }
  }

  const isChatLoading =
    isSendingMessage || isConversationLoading || isMessagesLoading || isResolvingEventConversation;
  const selectorValue =
    chatSelectionSource === 'creating' ? NEW_CONVERSATION_VALUE : (conversationId ?? '');

  const chatHeader = (
    <div className="space-y-3">
      <div className="space-y-1">
        <Label htmlFor="conversation-selector">Conversation history</Label>
        <select
          id="conversation-selector"
          value={selectorValue}
          onChange={(event) => handleConversationSelection(event.target.value)}
          className={denseSelectClassName}
          data-testid="conversation-selector"
        >
          <option value="">
            {eventChatContext
              ? `Current event context · ${formatAgentRole(eventChatContext.agentRole)} · ${eventChatContext.pipelineRunId}`
              : 'Select a conversation'}
          </option>
          {conversations.map((conversation) => (
            <option key={conversation.id} value={conversation.id}>
              {formatConversationLabel(conversation)}
            </option>
          ))}
          <option value={NEW_CONVERSATION_VALUE}>New conversation…</option>
        </select>
      </div>

      {selectedConversation ? (
        <div
          className="rounded-md border border-border bg-background px-3 py-3 text-xs text-muted-foreground"
          data-testid="conversation-summary"
        >
          <p className="font-medium text-foreground">
            {selectedConversation.title ?? formatAgentRole(selectedConversation.agent_role)}
          </p>
          <p className="mt-1 font-mono uppercase tracking-[0.14em]">
            {formatConversationLabel(selectedConversation)}
          </p>
        </div>
      ) : null}

      {selectedConversationOutsideEventContext ? (
        <p className="text-xs text-muted-foreground" data-testid="conversation-context-note">
          Viewing conversation outside selected event context.
        </p>
      ) : null}

      {chatSelectionSource === 'creating' ? (
        <form
          className="space-y-3 rounded-md border border-border bg-background p-3"
          onSubmit={handleCreateConversation}
          data-testid="new-conversation-form"
        >
          <div className="grid gap-3 md:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor="new-conversation-run">Pipeline run</Label>
              <select
                id="new-conversation-run"
                value={createConversationDraft.pipelineRunId}
                onChange={(event) =>
                  setCreateConversationDraft((current) => ({
                    ...current,
                    pipelineRunId: event.target.value,
                  }))
                }
                className={denseSelectClassName}
                data-testid="new-conversation-run"
              >
                <option value="">Select a pipeline run</option>
                {runOptions.map((run) => (
                  <option key={run.id} value={run.id}>
                    {run.label}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-1">
              <Label htmlFor="new-conversation-agent-role">Agent role</Label>
              <select
                id="new-conversation-agent-role"
                value={createConversationDraft.agentRole}
                onChange={(event) =>
                  setCreateConversationDraft((current) => ({
                    ...current,
                    agentRole: event.target.value as AgentRole | '',
                  }))
                }
                className={denseSelectClassName}
                data-testid="new-conversation-agent-role"
              >
                <option value="">Select an agent role</option>
                {AGENT_ROLE_OPTIONS.map((role) => (
                  <option key={role} value={role}>
                    {formatAgentRole(role)}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {isRunsLoading ? (
            <p className="text-xs text-muted-foreground" data-testid="new-conversation-loading">
              Loading pipeline runs…
            </p>
          ) : runOptions.length === 0 ? (
            <p className="text-xs text-muted-foreground" data-testid="new-conversation-empty">
              No pipeline runs available yet.
            </p>
          ) : null}

          <div className="flex flex-wrap gap-2">
            <Button
              type="submit"
              size="dense"
              disabled={
                isCreatingConversation ||
                !createConversationDraft.pipelineRunId ||
                !createConversationDraft.agentRole
              }
              data-testid="new-conversation-submit"
            >
              {isCreatingConversation ? 'Creating…' : 'Create conversation'}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="dense"
              onClick={() => {
                setChatSelectionSource(selectedConversation ? 'manual' : 'event');
                setChatError(null);
              }}
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  );

  return (
    <div className="space-y-4" data-testid="realtime-page">
      <PageHeader
        eyebrow="Realtime"
        title="Execution feed"
        description="Monitor live pipeline events, inspect payloads, and converse with agent context without leaving the console."
        meta={<Badge variant="outline">{events.length} events</Badge>}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {!autoScroll && events.length > 0 ? (
              <Button
                type="button"
                variant="outline"
                size="dense"
                onClick={resumeScroll}
                data-testid="realtime-resume-scroll"
              >
                <ArrowDown className="size-4" />
                Resume live scroll
              </Button>
            ) : null}
            <Badge variant={status === 'open' ? 'success' : 'outline'}>{status}</Badge>
          </div>
        }
      />

      <div className="grid gap-4 xl:h-[calc(100vh-15rem)] xl:grid-cols-[minmax(360px,0.92fr)_minmax(0,1.08fr)]">
        <div className="flex min-h-115 min-w-0 flex-col rounded-lg border border-border bg-card p-4 xl:min-h-0">
          <div className="mb-4 flex items-center justify-between gap-3">
            <div className="space-y-1">
              <h3 className="text-lg font-semibold">Event feed</h3>
              <p className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                {status === 'open' ? 'Live websocket stream' : 'Connecting websocket stream'}
              </p>
            </div>
            <Badge variant={status === 'open' ? 'success' : 'outline'}>{status}</Badge>
          </div>

          <div
            ref={feedRef}
            onScroll={handleFeedScroll}
            className="flex-1 space-y-2 overflow-y-auto pr-1"
            data-testid="realtime-feed"
          >
            {isLoading && events.length === 0 ? (
              <div className="space-y-2" data-testid="realtime-loading">
                {Array.from({ length: 4 }).map((_, index) => (
                  <div
                    key={index}
                    className="h-24 animate-pulse rounded-lg border border-border bg-muted/40"
                  />
                ))}
              </div>
            ) : events.length === 0 ? (
              <div className="flex h-full min-h-48 items-center justify-center rounded-lg border border-dashed border-border bg-background">
                <p className="text-sm text-muted-foreground" data-testid="realtime-empty">
                  No events yet.
                </p>
              </div>
            ) : (
              events.map((event) => {
                const Icon = eventIcon(event.event_kind);
                const isSelected = selectedEventId === event.id;

                return (
                  <button
                    key={event.id}
                    type="button"
                    className={cn(
                      'flex w-full flex-col gap-3 rounded-lg border border-border bg-background p-3 text-left transition-colors hover:border-primary/15 hover:bg-accent/45',
                      isSelected &&
                        'border-primary/40 bg-accent/55 shadow-[0_0_0_1px_rgba(96,165,250,0.22)]',
                    )}
                    onClick={() => {
                      setUserSelectedEventId(event.id);
                      setSelectedEventId(event.id);
                    }}
                    data-testid={`event-card-${event.id}`}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex min-w-0 items-center gap-2">
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border bg-card text-muted-foreground">
                          <Icon className="size-4" />
                        </div>
                        <div className="flex min-w-0 flex-wrap items-center gap-2">
                          <Badge variant={eventVariant(event.event_kind)}>
                            {eventLabel(event.event_kind)}
                          </Badge>
                          {event.agent_role ? (
                            <Badge variant="outline">{event.agent_role}</Badge>
                          ) : null}
                          {event.live ? <Badge variant="success">live</Badge> : null}
                        </div>
                      </div>
                      <time
                        className="shrink-0 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground"
                        dateTime={event.created_at}
                      >
                        {relativeTime(event.created_at)}
                      </time>
                    </div>
                    <div>
                      <p className="font-medium text-foreground">{event.title}</p>
                      {event.summary ? (
                        <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                          {event.summary}
                        </p>
                      ) : null}
                    </div>
                    <div className="flex flex-wrap gap-x-3 gap-y-1 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                      <span>run · {event.pipeline_run_id ?? 'none'}</span>
                      <span>strategy · {event.strategy_id ?? 'none'}</span>
                    </div>
                  </button>
                );
              })
            )}
          </div>
        </div>

        <div className="flex min-h-115 w-full flex-1 flex-col rounded-lg border border-border bg-card p-4 xl:min-h-0">
          {selectedEvent ? (
            <div
              className="flex min-h-0 flex-1 flex-col space-y-4"
              data-testid="selected-event-panel"
            >
              <div className="space-y-3 rounded-lg border border-border bg-background p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={eventVariant(selectedEvent.event_kind)}>
                    {eventLabel(selectedEvent.event_kind)}
                  </Badge>
                  {selectedEvent.agent_role ? (
                    <Badge variant="outline">{selectedEvent.agent_role}</Badge>
                  ) : null}
                  {selectedEvent.live ? <Badge variant="success">live</Badge> : null}
                </div>
                <div className="space-y-1">
                  <h3 className="text-lg font-semibold text-foreground">{selectedEvent.title}</h3>
                  <p className="font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                    {new Date(selectedEvent.created_at).toLocaleString()}
                  </p>
                </div>

                {selectedEvent.summary ? (
                  <div className="rounded-md border border-border bg-background p-3">
                    <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                      Summary
                    </h4>
                    <p className="mt-2 text-sm text-muted-foreground">{selectedEvent.summary}</p>
                  </div>
                ) : null}

                <dl className="grid gap-3 sm:grid-cols-2">
                  <div className="rounded-md border border-border bg-background p-3">
                    <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                      Pipeline run
                    </dt>
                    <dd className="mt-2 font-mono text-[13px] font-medium text-foreground">
                      {selectedEvent.pipeline_run_id ?? '—'}
                    </dd>
                  </div>
                  <div className="rounded-md border border-border bg-background p-3">
                    <dt className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                      Strategy
                    </dt>
                    <dd className="mt-2 font-mono text-[13px] font-medium text-foreground">
                      {selectedEvent.strategy_id ?? '—'}
                    </dd>
                  </div>
                </dl>

                <div>
                  <h4 className="mb-2 font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                    Event payload
                  </h4>
                  <pre
                    className="max-h-56 overflow-x-auto rounded-md border border-border bg-background p-3 font-mono text-[12px] text-muted-foreground"
                    data-testid="selected-event-metadata"
                  >
                    {JSON.stringify(selectedEvent.metadata ?? selectedEvent, null, 2)}
                  </pre>
                </div>
              </div>

              <div className="flex min-h-0 flex-1 flex-col">
                <div className="mb-2 flex items-center justify-between gap-3">
                  <h4 className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                    Conversation
                  </h4>
                  {activeChatContext ? (
                    <p className="font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                      {formatAgentRole(activeChatContext.agentRole)} ·{' '}
                      {activeChatContext.pipelineRunId}
                    </p>
                  ) : null}
                </div>
                {!activeChatContext ? (
                  <p className="mb-3 text-sm text-muted-foreground" data-testid="chat-unavailable">
                    Select an agent event or conversation to chat.
                  </p>
                ) : null}
                {resolvedChatError ? (
                  <p className="mb-3 text-sm text-destructive" data-testid="chat-error">
                    {resolvedChatError}
                  </p>
                ) : null}
                <div className="min-h-0 flex-1 overflow-hidden rounded-lg border border-border bg-background">
                  <ChatPanel
                    header={chatHeader}
                    messages={visibleChatMessages}
                    onSendMessage={activeChatContext ? handleSendMessage : undefined}
                    isLoading={activeChatContext ? isChatLoading : false}
                  />
                </div>
              </div>
            </div>
          ) : (
            <div className="flex h-full items-center justify-center rounded-lg border border-dashed border-border bg-background">
              <p className="text-sm text-muted-foreground">
                Select an event to start a conversation
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
