export function RealtimePage() {
  return (
    <div
      className="flex h-auto flex-col gap-4 md:h-[calc(100vh-12rem)] md:flex-row"
      data-testid="realtime-page"
    >
      {/* Left panel: event feed */}
      <div className="w-full min-w-0 rounded-lg border bg-card p-4 md:w-2/5">
        <h3 className="mb-4 text-lg font-semibold">Event Feed</h3>
        <p className="text-sm text-muted-foreground">Events will appear here.</p>
      </div>

      {/* Right panel: conversation */}
      <div className="flex w-full flex-1 items-center justify-center rounded-lg border bg-card p-4">
        <p className="text-sm text-muted-foreground">Select an event to start a conversation</p>
      </div>
    </div>
  )
}
