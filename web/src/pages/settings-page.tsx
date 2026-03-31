import { type FormEvent, useEffect, useState } from 'react'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cpu, Power, Save, Settings2, ShieldAlert } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { apiClient, ApiClientError } from '@/lib/api/client'
import type {
  EngineStatus,
  LLMProviderSettingsGroup,
  LLMProviderUpdateRequest,
  Settings,
  SettingsUpdateRequest,
} from '@/lib/api/types'

type ProviderKey = Exclude<keyof LLMProviderSettingsGroup, 'ollama'>

interface EditableProviderState {
  api_key: string
  base_url: string
  model: string
}

interface SettingsFormState {
  llm: {
    default_provider: string
    deep_think_model: string
    quick_think_model: string
    providers: {
      openai: EditableProviderState
      anthropic: EditableProviderState
      google: EditableProviderState
      openrouter: EditableProviderState
      xai: EditableProviderState
      ollama: {
        base_url: string
        model: string
      }
    }
  }
  risk: Settings['risk']
}

const providerDefinitions: Array<{ key: ProviderKey; label: string; supportsBaseUrl?: boolean }> = [
  { key: 'openai', label: 'OpenAI', supportsBaseUrl: true },
  { key: 'anthropic', label: 'Anthropic' },
  { key: 'google', label: 'Google' },
  { key: 'openrouter', label: 'OpenRouter', supportsBaseUrl: true },
  { key: 'xai', label: 'xAI', supportsBaseUrl: true },
]

function toFormState(settings: Settings): SettingsFormState {
  return {
    llm: {
      default_provider: settings.llm.default_provider,
      deep_think_model: settings.llm.deep_think_model,
      quick_think_model: settings.llm.quick_think_model,
      providers: {
        openai: {
          api_key: '',
          base_url: settings.llm.providers.openai.base_url ?? '',
          model: settings.llm.providers.openai.model,
        },
        anthropic: {
          api_key: '',
          base_url: settings.llm.providers.anthropic.base_url ?? '',
          model: settings.llm.providers.anthropic.model,
        },
        google: {
          api_key: '',
          base_url: settings.llm.providers.google.base_url ?? '',
          model: settings.llm.providers.google.model,
        },
        openrouter: {
          api_key: '',
          base_url: settings.llm.providers.openrouter.base_url ?? '',
          model: settings.llm.providers.openrouter.model,
        },
        xai: {
          api_key: '',
          base_url: settings.llm.providers.xai.base_url ?? '',
          model: settings.llm.providers.xai.model,
        },
        ollama: {
          base_url: settings.llm.providers.ollama.base_url ?? '',
          model: settings.llm.providers.ollama.model,
        },
      },
    },
    risk: settings.risk,
  }
}

function buildProviderUpdate(provider: EditableProviderState): LLMProviderUpdateRequest {
  const apiKey = provider.api_key.trim()

  return {
    model: provider.model.trim(),
    base_url: provider.base_url.trim() || undefined,
    ...(apiKey ? { api_key: apiKey } : {}),
  }
}

function formatUptime(totalSeconds: number) {
  const days = Math.floor(totalSeconds / 86_400)
  const hours = Math.floor((totalSeconds % 86_400) / 3_600)
  const minutes = Math.floor((totalSeconds % 3_600) / 60)

  if (days > 0) {
    return `${days}d ${hours}h`
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }
  return `${Math.max(minutes, 0)}m`
}

function RiskStatusSummary({ riskStatus }: { riskStatus: EngineStatus }) {
  return (
    <div className="grid gap-3 rounded-lg border p-4 text-sm sm:grid-cols-3">
      <div>
        <p className="text-muted-foreground">Risk status</p>
        <p className="font-medium capitalize">{riskStatus.risk_status}</p>
      </div>
      <div>
        <p className="text-muted-foreground">Circuit breaker</p>
        <p className="font-medium capitalize">{riskStatus.circuit_breaker.state.replace('_', ' ')}</p>
      </div>
      <div>
        <p className="text-muted-foreground">Live position cap</p>
        <p className="font-medium">{riskStatus.position_limits.max_per_position_pct}%</p>
      </div>
    </div>
  )
}

export function SettingsPage() {
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<SettingsFormState | null>(null)
  const [saveMessage, setSaveMessage] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => apiClient.getSettings(),
  })

  const riskQuery = useQuery({
    queryKey: ['risk', 'status'],
    queryFn: () => apiClient.getRiskStatus(),
    refetchInterval: 15_000,
  })

  useEffect(() => {
    if (settingsQuery.data) {
      setFormState(toFormState(settingsQuery.data))
    }
  }, [settingsQuery.data])

  function updateFormState(updater: (current: SettingsFormState) => SettingsFormState) {
    setFormState((current) => (current ? updater(current) : current))
    setSaveMessage(null)
    setSaveError(null)
  }

  const saveMutation = useMutation({
    mutationFn: (payload: SettingsUpdateRequest) => apiClient.updateSettings(payload),
    onSuccess: (updatedSettings) => {
      queryClient.setQueryData(['settings'], updatedSettings)
      setFormState(toFormState(updatedSettings))
      setSaveMessage('Settings saved.')
      setSaveError(null)
    },
    onError: (error) => {
      setSaveMessage(null)
      setSaveError(error instanceof ApiClientError ? error.message : 'Unable to save settings.')
    },
  })

  const killSwitchMutation = useMutation({
    mutationFn: (active: boolean) =>
      apiClient.toggleKillSwitch({
        active,
        reason: active ? 'Activated from settings page' : 'Deactivated from settings page',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['risk', 'status'] })
    },
  })

  if (settingsQuery.isError || (!settingsQuery.isLoading && !settingsQuery.data)) {
    return (
      <Card data-testid="settings-page-error">
        <CardHeader>
          <CardTitle>Settings</CardTitle>
          <CardDescription>Unable to load the current system configuration.</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (settingsQuery.isLoading || !formState) {
    return (
      <div className="space-y-6" data-testid="settings-page-loading">
        <div className="h-24 animate-pulse rounded-2xl border bg-card" />
        <div className="grid gap-6 lg:grid-cols-2">
          <div className="h-72 animate-pulse rounded-2xl border bg-card" />
          <div className="h-72 animate-pulse rounded-2xl border bg-card" />
        </div>
      </div>
    )
  }

  const settingsData = settingsQuery.data!
  const connectedBrokers = settingsData.system.connected_brokers ?? []

  function handleProviderChange<K extends ProviderKey>(
    providerKey: K,
    field: keyof EditableProviderState,
    value: string,
  ) {
    updateFormState((current) => ({
      ...current,
      llm: {
        ...current.llm,
        providers: {
          ...current.llm.providers,
          [providerKey]: {
            ...current.llm.providers[providerKey],
            [field]: value,
          },
        },
      },
    }))
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!formState) {
      return
    }

    const payload: SettingsUpdateRequest = {
      llm: {
        default_provider: formState.llm.default_provider.trim(),
        deep_think_model: formState.llm.deep_think_model.trim(),
        quick_think_model: formState.llm.quick_think_model.trim(),
        providers: {
          openai: buildProviderUpdate(formState.llm.providers.openai),
          anthropic: buildProviderUpdate(formState.llm.providers.anthropic),
          google: buildProviderUpdate(formState.llm.providers.google),
          openrouter: buildProviderUpdate(formState.llm.providers.openrouter),
          xai: buildProviderUpdate(formState.llm.providers.xai),
          ollama: {
            base_url: formState.llm.providers.ollama.base_url.trim() || undefined,
            model: formState.llm.providers.ollama.model.trim(),
          },
        },
      },
      risk: formState.risk,
    }

    saveMutation.mutate(payload)
  }

  return (
    <form className="space-y-6" onSubmit={handleSubmit} data-testid="settings-page">
      <Card>
        <CardHeader className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2">
              <Settings2 className="size-5" />
              Settings
            </CardTitle>
            <CardDescription>
              Configure LLM providers, risk guardrails, and runtime controls.
            </CardDescription>
          </div>
          <div className="flex flex-col items-start gap-2 sm:items-end">
            <Button type="submit" disabled={saveMutation.isPending} data-testid="settings-save-button">
              <Save className="size-4" />
              {saveMutation.isPending ? 'Saving…' : 'Save settings'}
            </Button>
            {saveMessage ? <p className="text-sm text-emerald-600">{saveMessage}</p> : null}
            {saveError ? <p className="text-sm text-destructive">{saveError}</p> : null}
          </div>
        </CardHeader>
      </Card>

      <div className="grid gap-6 xl:grid-cols-[1.4fr_0.9fr]">
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Cpu className="size-5" />
                Provider configuration
              </CardTitle>
              <CardDescription>Set model tiers and provider-specific credentials.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="grid gap-4 md:grid-cols-3">
                <div className="space-y-2">
                  <Label htmlFor="default-provider">Default provider</Label>
                  <select
                    id="default-provider"
                    value={formState.llm.default_provider}
                    onChange={(event) =>
                      updateFormState((current) => ({
                        ...current,
                        llm: { ...current.llm, default_provider: event.target.value },
                      }))
                    }
                    className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    {providerDefinitions.map(({ key, label }) => (
                      <option key={key} value={key}>
                        {label}
                      </option>
                    ))}
                    <option value="ollama">Ollama</option>
                  </select>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="deep-think-model">Deep think model</Label>
                  <Input
                    id="deep-think-model"
                    value={formState.llm.deep_think_model}
                    onChange={(event) =>
                      updateFormState((current) => ({
                        ...current,
                        llm: { ...current.llm, deep_think_model: event.target.value },
                      }))
                    }
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="quick-think-model">Quick think model</Label>
                  <Input
                    id="quick-think-model"
                    value={formState.llm.quick_think_model}
                    onChange={(event) =>
                      updateFormState((current) => ({
                        ...current,
                        llm: { ...current.llm, quick_think_model: event.target.value },
                      }))
                    }
                  />
                </div>
              </div>

              <div className="space-y-4">
                {providerDefinitions.map(({ key, label, supportsBaseUrl }) => {
                  const provider = formState.llm.providers[key]
                  const savedProvider = settingsData.llm.providers[key]
                  const hasSavedLast4 = Boolean(savedProvider.api_key_last4)
                  const keyStatus = provider.api_key.trim()
                    ? 'New key ready'
                    : savedProvider.api_key_configured
                      ? hasSavedLast4
                        ? `Configured ••••${savedProvider.api_key_last4}`
                        : 'Configured'
                      : 'Not set'

                  return (
                    <div key={key} className="rounded-xl border p-4" data-testid={`provider-${key}`}>
                      <div className="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                        <div>
                          <p className="font-medium">{label}</p>
                          <p className="text-sm text-muted-foreground">Provider-specific connection details.</p>
                        </div>
                        <Badge variant={provider.api_key.trim() || savedProvider.api_key_configured ? 'success' : 'outline'}>
                          {keyStatus}
                        </Badge>
                      </div>

                      <div className="grid gap-4 md:grid-cols-3">
                        <div className="space-y-2">
                          <Label htmlFor={`${key}-api-key`}>API key</Label>
                          <Input
                            id={`${key}-api-key`}
                            type="password"
                            value={provider.api_key}
                            placeholder={savedProvider.api_key_last4 ? `••••${savedProvider.api_key_last4}` : 'Enter new API key'}
                            onChange={(event) => handleProviderChange(key, 'api_key', event.target.value)}
                          />
                        </div>

                        {supportsBaseUrl ? (
                          <div className="space-y-2">
                            <Label htmlFor={`${key}-base-url`}>Base URL</Label>
                            <Input
                              id={`${key}-base-url`}
                              value={provider.base_url}
                              onChange={(event) => handleProviderChange(key, 'base_url', event.target.value)}
                            />
                          </div>
                        ) : (
                          <div className="space-y-2">
                            <Label htmlFor={`${key}-base-url`}>Base URL</Label>
                            <Input id={`${key}-base-url`} value={provider.base_url} disabled placeholder="Provider default" />
                          </div>
                        )}

                        <div className="space-y-2">
                          <Label htmlFor={`${key}-model`}>Model</Label>
                          <Input
                            id={`${key}-model`}
                            value={provider.model}
                            onChange={(event) => handleProviderChange(key, 'model', event.target.value)}
                          />
                        </div>
                      </div>
                    </div>
                  )
                })}

                <div className="rounded-xl border p-4" data-testid="provider-ollama">
                  <div className="mb-4">
                    <p className="font-medium">Ollama</p>
                    <p className="text-sm text-muted-foreground">Local model endpoint configuration.</p>
                  </div>

                  <div className="grid gap-4 md:grid-cols-2">
                    <div className="space-y-2">
                      <Label htmlFor="ollama-base-url">Base URL</Label>
                      <Input
                        id="ollama-base-url"
                        value={formState.llm.providers.ollama.base_url}
                        onChange={(event) =>
                          updateFormState((current) => ({
                            ...current,
                            llm: {
                              ...current.llm,
                              providers: {
                                ...current.llm.providers,
                                ollama: {
                                  ...current.llm.providers.ollama,
                                  base_url: event.target.value,
                                },
                              },
                            },
                          }))
                        }
                      />
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="ollama-model">Model</Label>
                      <Input
                        id="ollama-model"
                        value={formState.llm.providers.ollama.model}
                        onChange={(event) =>
                          updateFormState((current) => ({
                            ...current,
                            llm: {
                              ...current.llm,
                              providers: {
                                ...current.llm.providers,
                                ollama: {
                                  ...current.llm.providers.ollama,
                                  model: event.target.value,
                                },
                              },
                            },
                          }))
                        }
                      />
                    </div>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <ShieldAlert className="size-5" />
                Risk limits
              </CardTitle>
              <CardDescription>Adjust circuit breaker thresholds and portfolio exposure caps.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 sm:grid-cols-2">
                {[
                  {
                    id: 'max-position-size',
                    label: 'Max position size (%)',
                    value: formState.risk.max_position_size_pct,
                    field: 'max_position_size_pct' as const,
                    step: '0.1',
                  },
                  {
                    id: 'max-daily-loss',
                    label: 'Max daily loss (%)',
                    value: formState.risk.max_daily_loss_pct,
                    field: 'max_daily_loss_pct' as const,
                    step: '0.1',
                  },
                  {
                    id: 'max-drawdown',
                    label: 'Max drawdown (%)',
                    value: formState.risk.max_drawdown_pct,
                    field: 'max_drawdown_pct' as const,
                    step: '0.1',
                  },
                  {
                    id: 'max-open-positions',
                    label: 'Max open positions',
                    value: formState.risk.max_open_positions,
                    field: 'max_open_positions' as const,
                    step: '1',
                  },
                  {
                    id: 'max-total-exposure',
                    label: 'Max total exposure (%)',
                    value: formState.risk.max_total_exposure_pct,
                    field: 'max_total_exposure_pct' as const,
                    step: '0.1',
                  },
                  {
                    id: 'max-per-market-exposure',
                    label: 'Max per market exposure (%)',
                    value: formState.risk.max_per_market_exposure_pct,
                    field: 'max_per_market_exposure_pct' as const,
                    step: '0.1',
                  },
                  {
                    id: 'circuit-breaker-threshold',
                    label: 'Circuit breaker threshold (%)',
                    value: formState.risk.circuit_breaker_threshold_pct,
                    field: 'circuit_breaker_threshold_pct' as const,
                    step: '0.1',
                  },
                  {
                    id: 'circuit-breaker-cooldown',
                    label: 'Circuit breaker cooldown (min)',
                    value: formState.risk.circuit_breaker_cooldown_min,
                    field: 'circuit_breaker_cooldown_min' as const,
                    step: '1',
                  },
                ].map(({ id, label, value, field, step }) => (
                  <div key={id} className="space-y-2">
                    <Label htmlFor={id}>{label}</Label>
                    <Input
                      id={id}
                      type="number"
                      step={step}
                      value={value}
                      onChange={(event) =>
                        updateFormState((current) => ({
                          ...current,
                          risk: {
                            ...current.risk,
                            [field]:
                              field === 'max_open_positions' || field === 'circuit_breaker_cooldown_min'
                                ? Number.parseInt(event.target.value || '0', 10)
                                : Number.parseFloat(event.target.value || '0'),
                          },
                        }))
                      }
                    />
                  </div>
                ))}
              </div>

              {riskQuery.data ? <RiskStatusSummary riskStatus={riskQuery.data} /> : null}
            </CardContent>
          </Card>
        </div>

        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Power className="size-5" />
                Kill switch
              </CardTitle>
              <CardDescription>Immediately halt or resume trading activity.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {riskQuery.isLoading ? (
                <div className="h-24 animate-pulse rounded-lg border bg-muted/40" />
              ) : riskQuery.isError || !riskQuery.data ? (
                <p className="text-sm text-muted-foreground">
                  Unable to load the live risk engine status.
                </p>
              ) : (
                <div className="rounded-lg border p-4">
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <p className="font-medium">
                        {riskQuery.data.kill_switch.active ? 'Trading halted' : 'Trading enabled'}
                      </p>
                      <p className="mt-1 text-sm text-muted-foreground">
                        {riskQuery.data.kill_switch.active
                          ? (riskQuery.data.kill_switch.reason && riskQuery.data.kill_switch.reason.trim()) ||
                            'All orders are blocked.'
                          : 'The engine can submit orders normally.'}
                      </p>
                    </div>

                    <Button
                      type="button"
                      variant={riskQuery.data.kill_switch.active ? 'outline' : 'default'}
                      disabled={killSwitchMutation.isPending}
                      onClick={() => killSwitchMutation.mutate(!riskQuery.data.kill_switch.active)}
                      data-testid="settings-kill-switch-button"
                    >
                      {killSwitchMutation.isPending
                        ? 'Updating…'
                        : riskQuery.data.kill_switch.active
                          ? 'Deactivate'
                          : 'Activate'}
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>System info</CardTitle>
              <CardDescription>Runtime details and connected broker status.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4" data-testid="system-info">
              <div className="grid gap-4 sm:grid-cols-3">
                <div>
                  <p className="text-sm text-muted-foreground">Environment</p>
                  <p className="font-medium capitalize">{settingsData.system.environment || 'unknown'}</p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Version</p>
                  <p className="font-medium">{settingsData.system.version}</p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Uptime</p>
                  <p className="font-medium">{formatUptime(settingsData.system.uptime_seconds)}</p>
                </div>
              </div>

              <div className="space-y-3">
                <p className="text-sm font-medium">Connected brokers</p>
                <div className="space-y-2">
                  {connectedBrokers.length === 0 ? (
                    <p className="rounded-lg border px-3 py-2 text-sm text-muted-foreground">
                      No connected brokers reported.
                    </p>
                  ) : (
                    connectedBrokers.map((broker) => (
                      <div key={broker.name} className="flex items-center justify-between rounded-lg border px-3 py-2">
                        <div>
                          <p className="font-medium capitalize">{broker.name}</p>
                          <p className="text-xs text-muted-foreground">
                            {broker.paper_mode ? 'Paper mode' : 'Live mode'}
                          </p>
                        </div>
                        <Badge variant={broker.configured ? 'success' : 'secondary'}>
                          {broker.configured ? 'Configured' : 'Not configured'}
                        </Badge>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </form>
  )
}
