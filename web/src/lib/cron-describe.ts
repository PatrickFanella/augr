const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

function formatHour(h: number): string {
  if (h === 0) return '12 AM'
  if (h === 12) return '12 PM'
  return h < 12 ? `${h} AM` : `${h - 12} PM`
}

function formatTime(h: number, m: number): string {
  const mm = String(m).padStart(2, '0')
  if (h === 0) return `12:${mm} AM`
  if (h === 12) return `12:${mm} PM`
  return h < 12 ? `${h}:${mm} AM` : `${h - 12}:${mm} PM`
}

function describeDow(dow: string): string {
  if (dow === '*') return ''
  if (dow === '1-5') return 'Mon\u2013Fri'
  if (dow === '0,6' || dow === '6,0') return 'Weekends'
  return dow
    .split(',')
    .map((d) => {
      if (d.includes('-')) {
        const [start, end] = d.split('-').map(Number)
        return `${DAY_NAMES[start]}\u2013${DAY_NAMES[end]}`
      }
      return DAY_NAMES[Number(d)] ?? d
    })
    .join(', ')
}

/**
 * Convert a cron expression to human-readable English.
 *
 * "0 *​/4 * * 1-5" -> "Every 4 hours, Mon-Fri"
 * "30 10 * * 1-5" -> "Daily at 10:30 AM, Mon-Fri"
 * "" -> "Manual only"
 */
export function describeCron(expr: string): string {
  const trimmed = expr.trim()
  if (!trimmed) return 'Manual only'

  const parts = trimmed.split(/\s+/)
  if (parts.length !== 5) return trimmed

  const [min, hour, , , dow] = parts

  let timePart: string

  const hourStep = hour.match(/^\*\/(\d+)$/)
  const minStep = min.match(/^\*\/(\d+)$/)

  if (hourStep) {
    const n = Number(hourStep[1])
    timePart = n === 1 ? 'Every hour' : `Every ${n} hours`
  } else if (minStep) {
    const n = Number(minStep[1])
    timePart = n === 1 ? 'Every minute' : `Every ${n} minutes`
  } else if (hour !== '*' && min !== '*') {
    const h = Number(hour)
    const m = Number(min)
    timePart = m === 0 ? `Daily at ${formatHour(h)}` : `Daily at ${formatTime(h, m)}`
  } else if (hour !== '*') {
    timePart = `Daily at ${formatHour(Number(hour))}`
  } else {
    timePart = 'Every minute'
  }

  const dayPart = describeDow(dow)
  return dayPart ? `${timePart}, ${dayPart}` : timePart
}
