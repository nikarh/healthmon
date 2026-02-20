import type { CSSProperties } from 'react'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { List, useDynamicRowHeight } from 'react-window'
import logoUrl from './assets/logo.svg'
import './App.css'

interface Container {
  id: number
  name: string
  container_id: string
  image: string
  image_tag: string
  image_id: string
  created_at: string
  registered_at: string
  started_at: string
  status: string
  role: string
  caps: string[]
  read_only: boolean
  user: string
  present: boolean
  health_status: string
  health_failing_streak: number
  unhealthy_since: string
  restart_loop: boolean
  restart_streak: number
  restart_loop_since: string
  healthcheck: Healthcheck | null
}

interface Healthcheck {
  test: string[]
  interval: string
  timeout: string
  start_period: string
  start_interval: string
  retries: number
}

interface EventItem {
  id: number
  container_pk: number
  container: string
  container_id: string
  type: string
  message: string
  timestamp: string
  old_image: string
  new_image: string
  old_image_id: string
  new_image_id: string
  reason: string
  details: string
  exit_code?: number | null
}

interface AlertItem {
  id: number
  container_pk: number
  container: string
  container_id: string
  type: string
  message: string
  timestamp: string
  old_image: string
  new_image: string
  old_image_id: string
  new_image_id: string
  reason: string
  details: string
  exit_code?: number | null
}

interface EventListResponse {
  items: EventItem[]
  total: number
}

interface AlertListResponse {
  items: AlertItem[]
  total: number
}

interface EventUpdate {
  container: Container
  event?: EventItem | null
  alert?: AlertItem | null
  container_event_total?: number
  event_total?: number
  alert_total?: number
}

interface PageState {
  beforeId?: number
  loading: boolean
  done: boolean
}

const PAGE_SIZE = 20
type ViewMode = 'containers' | 'events' | 'alerts'

const statusClass = (status: string) => {
  const s = status.toLowerCase()
  if (s === 'healthy') return 'status-running'
  if (s === 'running') return 'status-running'
  if (s === 'exited' || s === 'dead') return 'status-down'
  return 'status-warn'
}

const eventSeverityClass = (event: EventItem) => {
  const type = event.type.toLowerCase()
  const reason = event.reason.toLowerCase()
  if (type === 'restart') return 'sev-red'
  if (type === 'stopped' && event.exit_code != null && event.exit_code !== 0) return 'sev-red'
  if (reason === 'oom' || reason === 'die') return 'sev-red'
  return 'sev-blue'
}

const alertSeverityClass = (alert: AlertItem) => {
  const type = alert.type.toLowerCase()
  if (type === 'healthy' || type === 'restart_healed') return 'sev-green'
  if (type === 'image_changed' || type === 'recreated') return 'sev-blue'
  return 'sev-red'
}

const formatDate = (val: string) => {
  if (!val) return '—'
  return new Date(val).toLocaleString(undefined, {
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

const formatRelativeTime = (val: string) => {
  if (!val) return '—'
  if (val.startsWith('0001-01-01')) return '—'
  const date = new Date(val)
  if (Number.isNaN(date.getTime())) return '—'
  const diff = date.getTime() - Date.now()
  const abs = Math.abs(diff)
  const minute = 60 * 1000
  const hour = 60 * minute
  const day = 24 * hour
  const week = 7 * day
  const month = 30 * day
  const year = 365 * day
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  if (abs >= year) return rtf.format(Math.round(diff / year), 'year')
  if (abs >= month) return rtf.format(Math.round(diff / month), 'month')
  if (abs >= week) return rtf.format(Math.round(diff / week), 'week')
  if (abs >= day) return rtf.format(Math.round(diff / day), 'day')
  if (abs >= hour) return rtf.format(Math.round(diff / hour), 'hour')
  if (abs >= minute) return rtf.format(Math.round(diff / minute), 'minute')
  return rtf.format(Math.round(diff / 1000), 'second')
}

const displayStatus = (container: Container) => {
  const status = container.status.toLowerCase()
  const health = container.health_status.toLowerCase()
  if (status === 'running' && health === 'healthy') {
    return 'healthy'
  }
  return container.status
}

const hasDerivedFailure = (container: Container) => {
  if (container.restart_loop) return true
  return container.health_status.toLowerCase() === 'unhealthy'
}

const isContainerBroken = (container: Container) => {
  if (hasDerivedFailure(container)) return true
  if (container.role !== 'task') {
    return container.status.toLowerCase() !== 'running'
  }
  return container.status.toLowerCase() === 'dead'
}

const shortId = (val: string, max = 12) => {
  if (!val) return ''
  if (val.length <= max) return val
  return `${val.slice(0, max)}…`
}

const toTitle = (val: string) => {
  if (!val) return ''
  return val.replace(/[_-]+/g, ' ').replace(/\s+/g, ' ').trim().toUpperCase()
}

const singleWord = (val: string) => {
  if (!val) return ''
  const cleaned = val.trim()
  if (!cleaned) return ''
  return /\s/.test(cleaned) ? '' : cleaned
}

const deriveEventTitle = (event: EventItem) => {
  const reason = singleWord(event.reason)
  if (reason) return toTitle(reason)
  const type = singleWord(event.type)
  if (type) return toTitle(type)
  if (event.type) return toTitle(event.type)
  return 'EVENT'
}

const deriveChangeLine = (event: EventItem) => {
  if (event.old_image || event.new_image) {
    return `${event.old_image} → ${event.new_image}`.trim()
  }
  const message = event.message || ''
  if (!message) return ''
  if (message.toLowerCase().includes('signal ')) {
    return message
  }
  if (message.includes('->')) {
    return message
  }
  const regex = /from\\s+(.+?)\\s+to\\s+(.+)/i
  const match = regex.exec(message)
  if (match) {
    return `${match[1]} → ${match[2]}`
  }
  return ''
}

const deriveContainerLine = (event: EventItem) => {
  const container = event.container || ''
  const id = event.container_id ? `(${shortId(event.container_id)})` : ''
  return `${container} ${id}`.trim()
}

const deriveAlertTitle = (alert: AlertItem) => {
  const reason = singleWord(alert.reason)
  if (reason) return toTitle(reason)
  const type = singleWord(alert.type)
  if (type) return toTitle(type)
  if (alert.type) return toTitle(alert.type)
  return 'ALERT'
}

const deriveAlertChangeLine = (alert: AlertItem) => {
  if (alert.old_image || alert.new_image) {
    return `${alert.old_image} → ${alert.new_image}`.trim()
  }
  const message = alert.message || ''
  if (!message) return ''
  if (message.includes('->')) {
    return message
  }
  const regex = /from\\s+(.+?)\\s+to\\s+(.+)/i
  const match = regex.exec(message)
  if (match) {
    return `${match[1]} → ${match[2]}`
  }
  return ''
}

const deriveAlertContainerLine = (alert: AlertItem) => {
  const container = alert.container || ''
  const id = alert.container_id ? `(${shortId(alert.container_id)})` : ''
  return `${container} ${id}`.trim()
}

const parseAlertRestartCount = (alert: AlertItem) => {
  if (!alert.details) return null
  try {
    const parsed = JSON.parse(alert.details) as { restart_count?: number }
    if (typeof parsed.restart_count !== 'number' || parsed.restart_count <= 0) return null
    return parsed.restart_count
  } catch {
    return null
  }
}

const deriveDerivedStatus = (container: Container) => {
  if (container.restart_loop) {
    return {
      label: 'Restart loop',
      detail: `${String(container.restart_streak)} restarts`,
      severity: 'sev-red',
    }
  }
  const health = container.health_status.toLowerCase()
  if (health === 'unhealthy') {
    return {
      label: 'Unhealthy',
      detail: `${String(container.health_failing_streak)} failed checks`,
      severity: 'sev-red',
    }
  }
  return null
}

export default function App() {
  const [containers, setContainers] = useState<Container[]>([])
  const [expanded, setExpanded] = useState<Record<string, boolean | undefined>>({})
  const [events, setEvents] = useState<Record<string, EventItem[] | undefined>>({})
  const [pages, setPages] = useState<Record<string, PageState | undefined>>({})
  const [flash, setFlash] = useState<Record<string, boolean | undefined>>({})
  const [query, setQuery] = useState('')
  const [view, setView] = useState<ViewMode>('containers')
  const [allEvents, setAllEvents] = useState<EventItem[]>([])
  const [allEventsPage, setAllEventsPage] = useState<PageState>({ loading: false, done: false })
  const [allEventsError, setAllEventsError] = useState<string | null>(null)
  const [alerts, setAlerts] = useState<AlertItem[]>([])
  const [alertsPage, setAlertsPage] = useState<PageState>({ loading: false, done: false })
  const [alertsError, setAlertsError] = useState<string | null>(null)
  const [eventTotals, setEventTotals] = useState<Record<string, number | undefined>>({})
  const [allEventsTotal, setAllEventsTotal] = useState(0)
  const [alertsTotal, setAlertsTotal] = useState(0)

  const sortedContainers = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    const filtered = normalized
      ? containers.filter((item) => item.name.toLowerCase().includes(normalized))
      : containers
    const services = filtered.filter((item) => item.role !== 'task')
    const tasks = filtered.filter((item) => item.role === 'task')

    const sortedServices = [...services].sort((a, b) => {
      const aRank = isContainerBroken(a) ? 0 : 1
      const bRank = isContainerBroken(b) ? 0 : 1
      if (aRank !== bRank) return aRank - bRank
      return a.name.localeCompare(b.name)
    })

    const sortedTasks = [...tasks].sort((a, b) => {
      const aRank = isContainerBroken(a) ? 0 : 1
      const bRank = isContainerBroken(b) ? 0 : 1
      if (aRank !== bRank) return aRank - bRank
      return a.name.localeCompare(b.name)
    })

    return { services: sortedServices, tasks: sortedTasks }
  }, [containers, query])

  const loadContainers = useCallback(async () => {
    const res = await fetch('/api/containers')
    if (!res.ok) return
    const data = (await res.json()) as Container[]
    setContainers(data)
  }, [])

  const loadEvents = useCallback(
    async (name: string) => {
      setPages((prev) => ({
        ...prev,
        [name]: {
          beforeId: prev[name]?.beforeId,
          loading: true,
          done: prev[name]?.done ?? false,
        },
      }))

      const beforeId = pages[name]?.beforeId
      const query = new URLSearchParams({
        limit: PAGE_SIZE.toString(),
      })
      if (beforeId) {
        query.set('before_id', beforeId.toString())
      }

      const res = await fetch(`/api/containers/${name}/events?${query.toString()}`)
      if (!res.ok) {
        setPages((prev) => ({
          ...prev,
          [name]: { ...(prev[name] ?? { loading: false, done: false }), loading: false },
        }))
        return
      }
      const payload = (await res.json()) as EventListResponse | EventItem[]
      const data = Array.isArray(payload) ? payload : payload.items
      const total = Array.isArray(payload) ? data.length : payload.total
      setEvents((prev) => ({
        ...prev,
        [name]: [...(prev[name] ?? []), ...data],
      }))
      setEventTotals((prev) => ({ ...prev, [name]: total }))
      setPages((prev) => ({
        ...prev,
        [name]: {
          beforeId: data.length ? data[data.length - 1].id : beforeId,
          loading: false,
          done: data.length < PAGE_SIZE,
        },
      }))
    },
    [pages],
  )

  const loadAllEvents = useCallback(async () => {
    if (allEventsPage.loading || allEventsPage.done) return
    setAllEventsError(null)
    setAllEventsPage((prev) => ({
      ...prev,
      loading: true,
    }))
    const beforeId = allEventsPage.beforeId
    const query = new URLSearchParams({
      limit: PAGE_SIZE.toString(),
    })
    if (beforeId) {
      query.set('before_id', beforeId.toString())
    }
    try {
      const res = await fetch(`/api/events?${query.toString()}`)
      if (!res.ok) {
        setAllEventsError('Unable to load events. Check connection and retry.')
        setAllEventsPage((prev) => ({ ...prev, loading: false, done: true }))
        return
      }
      const payload = (await res.json()) as EventListResponse | EventItem[]
      const data = Array.isArray(payload) ? payload : payload.items
      const total = Array.isArray(payload) ? data.length : payload.total
      setAllEvents((prev) => [...prev, ...data])
      setAllEventsTotal(total)
      setAllEventsPage({
        beforeId: data.length ? data[data.length - 1].id : beforeId,
        loading: false,
        done: data.length < PAGE_SIZE,
      })
    } catch {
      setAllEventsError('Unable to load events. Check connection and retry.')
      setAllEventsPage((prev) => ({ ...prev, loading: false, done: true }))
    }
  }, [allEventsPage.beforeId, allEventsPage.done, allEventsPage.loading])

  const loadAlerts = useCallback(async () => {
    if (alertsPage.loading || alertsPage.done) return
    setAlertsError(null)
    setAlertsPage((prev) => ({
      ...prev,
      loading: true,
    }))
    const beforeId = alertsPage.beforeId
    const query = new URLSearchParams({
      limit: PAGE_SIZE.toString(),
    })
    if (beforeId) {
      query.set('before_id', beforeId.toString())
    }
    try {
      const res = await fetch(`/api/alerts?${query.toString()}`)
      if (!res.ok) {
        setAlertsError('Unable to load alerts. Check connection and retry.')
        setAlertsPage((prev) => ({ ...prev, loading: false, done: true }))
        return
      }
      const payload = (await res.json()) as AlertListResponse | AlertItem[]
      const data = Array.isArray(payload) ? payload : payload.items
      const total = Array.isArray(payload) ? data.length : payload.total
      setAlerts((prev) => [...prev, ...data])
      setAlertsTotal(total)
      setAlertsPage({
        beforeId: data.length ? data[data.length - 1].id : beforeId,
        loading: false,
        done: data.length < PAGE_SIZE,
      })
    } catch {
      setAlertsError('Unable to load alerts. Check connection and retry.')
      setAlertsPage((prev) => ({ ...prev, loading: false, done: true }))
    }
  }, [alertsPage.beforeId, alertsPage.done, alertsPage.loading])

  const toggleExpanded = useCallback(
    (name: string) => {
      const nextExpanded = !(expanded[name] ?? false)
      setExpanded((prev) => ({ ...prev, [name]: nextExpanded }))
      if (nextExpanded && !(events[name]?.length ?? 0)) {
        void loadEvents(name)
      }
    },
    [expanded, events, loadEvents],
  )

  useEffect(() => {
    void loadContainers()
  }, [loadContainers])

  useEffect(() => {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${wsProtocol}://${window.location.host}/api/events/stream`)

    ws.onmessage = (event) => {
      if (typeof event.data !== 'string') return
      const update = JSON.parse(event.data) as EventUpdate
      if (!update.container.present) {
        const name = update.container.name
        setContainers((prev) => prev.filter((item) => item.name !== name))
        setExpanded((prev) => {
          const { [name]: removed, ...rest } = prev
          void removed
          return rest
        })
        setEvents((prev) => {
          const { [name]: removed, ...rest } = prev
          void removed
          return rest
        })
        setFlash((prev) => {
          const { [name]: removed, ...rest } = prev
          void removed
          return rest
        })
        setEventTotals((prev) => {
          const { [name]: removed, ...rest } = prev
          void removed
          return rest
        })
        return
      }

      setContainers((prev) => {
        const existing = prev.find((item) => item.name === update.container.name)
        if (!existing) {
          return [...prev, update.container]
        }
        return prev.map((item) => (item.name === update.container.name ? update.container : item))
      })

      setFlash((prev) => ({ ...prev, [update.container.name]: true }))
      window.setTimeout(() => {
        setFlash((prev) => ({ ...prev, [update.container.name]: false }))
      }, 800)

      const eventUpdate = update.event
      if (eventUpdate && eventUpdate.id > 0) {
        setEvents((prev) => {
          if (!(expanded[update.container.name] ?? false)) return prev
          const current = prev[update.container.name] ?? []
          if (current.some((item) => item.id === eventUpdate.id)) return prev
          return {
            ...prev,
            [update.container.name]: [eventUpdate, ...current],
          }
        })

        setAllEvents((prev) => {
          if (prev.some((item) => item.id === eventUpdate.id)) return prev
          return [eventUpdate, ...prev]
        })
        if (typeof update.event_total === 'number') {
          setAllEventsTotal(update.event_total)
        } else {
          setAllEventsTotal((prev) => prev + 1)
        }
        if (typeof update.container_event_total === 'number') {
          setEventTotals((prev) => ({
            ...prev,
            [update.container.name]: update.container_event_total,
          }))
        } else {
          setEventTotals((prev) => ({
            ...prev,
            [update.container.name]: (prev[update.container.name] ?? 0) + 1,
          }))
        }
      }

      const alertUpdate = update.alert
      if (alertUpdate && alertUpdate.id > 0) {
        setAlerts((prev) => {
          if (prev.some((item) => item.id === alertUpdate.id)) return prev
          return [alertUpdate, ...prev]
        })
        if (typeof update.alert_total === 'number') {
          setAlertsTotal(update.alert_total)
        } else {
          setAlertsTotal((prev) => prev + 1)
        }
      }
    }

    return () => {
      ws.close()
    }
  }, [expanded])

  const handleRefresh = () => {
    void loadContainers()
  }

  return (
    <div className="app">
      <header className="hero">
        <div className="title-row">
          <img className="logo" src={logoUrl} alt="Healthmon logo" />
          <div className="title-stack">
            <span className="tag">healthmon</span>
            <h1>
              <span className="title-full">Container Health Monitor</span>
              <span className="title-mobile">healthmon</span>
            </h1>
          </div>
        </div>
        <button className="refresh" type="button" onClick={handleRefresh}>
          Refresh
        </button>
      </header>

      <section className="container-list">
        <div className="view-tabs">
          <button
            className={`view-tab ${view === 'containers' ? 'active' : ''}`}
            type="button"
            onClick={() => {
              setView('containers')
            }}
          >
            Containers
          </button>
          <button
            className={`view-tab ${view === 'events' ? 'active' : ''}`}
            type="button"
            onClick={() => {
              setView('events')
              if (allEvents.length === 0 && !allEventsPage.loading && !allEventsPage.done) {
                void loadAllEvents()
              }
            }}
          >
            Events
          </button>
          <button
            className={`view-tab ${view === 'alerts' ? 'active' : ''}`}
            type="button"
            onClick={() => {
              setView('alerts')
              if (alerts.length === 0 && !alertsPage.loading && !alertsPage.done) {
                void loadAlerts()
              }
            }}
          >
            Alerts
          </button>
        </div>

        {view === 'containers' && (
          <>
            <div className="search-row">
              <input
                className="search-input"
                type="search"
                placeholder="Search containers"
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value)
                }}
              />
              {query && (
                <button
                  className="clear-search"
                  type="button"
                  onClick={() => {
                    setQuery('')
                  }}
                >
                  Clear
                </button>
              )}
            </div>
            {sortedContainers.services.length === 0 && sortedContainers.tasks.length === 0 && (
              <div className="empty">No containers found yet.</div>
            )}
            {sortedContainers.services.length > 0 && (
              <div className="section">
                <div className="section-header">
                  <h2>Services</h2>
                  <span>{sortedContainers.services.length}</span>
                </div>
                {sortedContainers.services.map((container) => (
                  <ContainerRow
                    key={container.name}
                    container={container}
                    expanded={expanded[container.name] ?? false}
                    flash={flash[container.name] ?? false}
                    onToggle={toggleExpanded}
                    events={events[container.name] ?? []}
                    eventsTotal={eventTotals[container.name]}
                    page={pages[container.name] ?? { loading: false, done: false }}
                    onLoadMore={loadEvents}
                  />
                ))}
              </div>
            )}
            {sortedContainers.tasks.length > 0 && (
              <div className="section">
                <div className="section-header">
                  <h2>Tasks</h2>
                  <span>{sortedContainers.tasks.length}</span>
                </div>
                {sortedContainers.tasks.map((container) => (
                  <ContainerRow
                    key={container.name}
                    container={container}
                    expanded={expanded[container.name] ?? false}
                    flash={flash[container.name] ?? false}
                    onToggle={toggleExpanded}
                    events={events[container.name] ?? []}
                    eventsTotal={eventTotals[container.name]}
                    page={pages[container.name] ?? { loading: false, done: false }}
                    onLoadMore={loadEvents}
                  />
                ))}
              </div>
            )}
          </>
        )}

        {view === 'events' && (
          <AllEventsFeed
            events={allEvents}
            total={allEventsTotal}
            page={allEventsPage}
            onLoadMore={loadAllEvents}
            error={allEventsError}
            onRetry={() => {
              setAllEventsError(null)
              setAllEventsPage((prev) => ({ ...prev, done: false }))
              void loadAllEvents()
            }}
          />
        )}

        {view === 'alerts' && (
          <AllAlertsFeed
            alerts={alerts}
            total={alertsTotal}
            page={alertsPage}
            onLoadMore={loadAlerts}
            error={alertsError}
            onRetry={() => {
              setAlertsError(null)
              setAlertsPage((prev) => ({ ...prev, done: false }))
              void loadAlerts()
            }}
          />
        )}
      </section>
    </div>
  )
}

interface RowProps {
  container: Container
  expanded: boolean
  flash: boolean
  onToggle: (name: string) => void
  events: EventItem[]
  eventsTotal?: number
  page: PageState
  onLoadMore: (name: string) => Promise<void>
}

function ContainerRow({
  container,
  expanded,
  flash,
  onToggle,
  events,
  eventsTotal,
  page,
  onLoadMore,
}: RowProps) {
  const sentinelRef = useRef<HTMLDivElement | null>(null)
  const statusText = displayStatus(container)
  const derivedStatus = deriveDerivedStatus(container)
  const wentBad = container.restart_loop
    ? formatRelativeTime(container.restart_loop_since)
    : container.health_status.toLowerCase() === 'unhealthy'
      ? formatRelativeTime(container.unhealthy_since)
      : null

  useEffect(() => {
    if (!expanded || page.done || page.loading) return undefined

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          void onLoadMore(container.name)
        }
      },
      { rootMargin: '120px' },
    )

    const sentinel = sentinelRef.current
    if (sentinel) observer.observe(sentinel)

    return () => {
      if (sentinel) observer.unobserve(sentinel)
      observer.disconnect()
    }
  }, [container.name, expanded, onLoadMore, page.done, page.loading])

  const hasFailure = hasDerivedFailure(container)
  return (
    <article className={`container-card ${expanded ? 'expanded' : ''} ${flash ? 'flash' : ''}`}>
      <button
        className="container-main"
        type="button"
        onClick={() => {
          onToggle(container.name)
        }}
      >
        <div className={`status-dot ${hasFailure ? 'status-down' : statusClass(statusText)}`} />
        <div className="container-info">
          <div className="name-row">
            <span className="name container-name">{container.name}</span>
            <span className="status-pill">{statusText}</span>
          </div>
          <div className="meta">
            <span className="image-name">
              {container.image}:{container.image_tag}
            </span>
          </div>
        </div>
        <div className="container-side">
          {derivedStatus && (
            <div className="derived-status">
              <span className={`event-dot ${derivedStatus.severity}`} />
              <div>
                <div className="event-type">{derivedStatus.label}</div>
                <div className="event-message">{derivedStatus.detail}</div>
              </div>
            </div>
          )}
          <div className="time-lines">
            <div className="started-time">Started: {formatRelativeTime(container.started_at)}</div>
            {wentBad && <div className="started-time">Went bad: {wentBad}</div>}
          </div>
        </div>
      </button>

      {expanded && (
        <div className="container-details">
          <div className="details-grid">
            <div>
              <h3>Runtime</h3>
              <p>Registered: {formatRelativeTime(container.registered_at)}</p>
              <p>Created: {formatRelativeTime(container.created_at)}</p>
              <p className={container.user === '0:0' ? 'warn-text' : undefined}>
                User: {container.user}
                {container.user === '0:0' && <span className="warn-badge">!</span>}
              </p>
              <p className={!container.read_only ? 'warn-text' : undefined}>
                Read-only: {container.read_only ? 'yes' : 'no'}
                {!container.read_only && <span className="warn-badge">!</span>}
              </p>
            </div>
            <div>
              <h3>Capabilities</h3>
              <p className="caps">{container.caps.length ? container.caps.join(', ') : 'none'}</p>
            </div>
            {container.healthcheck && (
              <div>
                <h3>Health</h3>
                <p>Status: {container.health_status || 'none'}</p>
                <p>Failing streak: {container.health_failing_streak || 0}</p>
                <p>Interval: {container.healthcheck.interval || '—'}</p>
                <p>Timeout: {container.healthcheck.timeout || '—'}</p>
                <p>Start period: {container.healthcheck.start_period || '—'}</p>
                <p>Start interval: {container.healthcheck.start_interval || '—'}</p>
                <p>Retries: {container.healthcheck.retries}</p>
                <p>
                  Test:{' '}
                  {container.healthcheck.test.length > 0
                    ? container.healthcheck.test.join(' ')
                    : '—'}
                </p>
              </div>
            )}
          </div>

          {(events.length > 0 || page.loading || !page.done) && (
            <div className="events">
              <div className="events-header">
                <h3>Events</h3>
                <span>{eventsTotal ?? events.length} total</span>
              </div>
              <div className="event-list">
                {events.map((event, index) => (
                  <div
                    key={event.id}
                    className={`event-row event-row-compact ${
                      index === events.length - 1 ? 'feed-row-last' : ''
                    }`}
                  >
                    <div className={`event-dot ${eventSeverityClass(event)}`} />
                    <div className="event-body">
                      <div className="event-top">
                        <span className="event-title">{deriveEventTitle(event)}</span>
                        <span className="event-time">{formatRelativeTime(event.timestamp)}</span>
                      </div>
                      <div className="event-identity">{deriveContainerLine(event)}</div>
                      {(() => {
                        const changeLine = deriveChangeLine(event)
                        return changeLine ? <div className="event-change">{changeLine}</div> : null
                      })()}
                      {event.exit_code != null && (
                        <div className="event-meta">Exit code: {event.exit_code}</div>
                      )}
                    </div>
                  </div>
                ))}
                {!page.done && <div ref={sentinelRef} className="event-sentinel" />}
              </div>
              {page.loading && <div className="loading">Loading more events…</div>}
            </div>
          )}
        </div>
      )}
    </article>
  )
}

interface AllEventsProps {
  events: EventItem[]
  total: number
  page: PageState
  onLoadMore: () => Promise<void>
  error: string | null
  onRetry: () => void
}

function AllEventsFeed({ events, total, page, onLoadMore, error, onRetry }: AllEventsProps) {
  const listRef = useRef<HTMLDivElement | null>(null)
  const rowHeight = useDynamicRowHeight({ defaultRowHeight: 136 })
  const [listSize, setListSize] = useState({ width: 0, maxHeight: 0 })
  const handleItemsRendered = useCallback(
    ({ stopIndex }: { stopIndex: number }) => {
      if (page.loading || page.done || error) return
      if (stopIndex >= events.length - 4) {
        void onLoadMore()
      }
    },
    [events.length, onLoadMore, page.done, page.loading, error],
  )

  const handleRowMeasured = useCallback(() => {
    setListSize((prev) => ({ ...prev }))
  }, [])

  useLayoutEffect(() => {
    const element = listRef.current
    if (!element) return undefined

    const updateSize = () => {
      const listRect = element.getBoundingClientRect()
      const sectionElement = element.closest('.container-list')
      const sectionStyle = sectionElement ? window.getComputedStyle(sectionElement) : null
      const sectionPaddingBottom = sectionStyle
        ? Number.parseFloat(sectionStyle.paddingBottom) || 0
        : 0
      const viewportHeight = document.documentElement.clientHeight
      const maxHeight = Math.max(200, viewportHeight - listRect.top - sectionPaddingBottom - 1)
      const width = listRect.width
      if (!maxHeight || !width) return
      setListSize({ width, maxHeight })
    }

    updateSize()
    const observer = new ResizeObserver(updateSize)
    observer.observe(element)
    window.addEventListener('resize', updateSize)

    return () => {
      observer.disconnect()
      window.removeEventListener('resize', updateSize)
    }
  }, [])

  const listHeight = useMemo(() => {
    if (events.length === 0 || listSize.maxHeight === 0) return 0
    const average = rowHeight.getAverageRowHeight() || 136
    let total = 0
    for (let index = 0; index < events.length; index += 1) {
      total += rowHeight.getRowHeight(index) ?? average
      if (total >= listSize.maxHeight) {
        return listSize.maxHeight
      }
    }
    return Math.min(total, listSize.maxHeight)
  }, [events.length, listSize.maxHeight, rowHeight])

  const rowComponent = useCallback(
    ({
      index,
      style,
      ariaAttributes,
    }: {
      index: number
      style: CSSProperties
      ariaAttributes: {
        'aria-posinset': number
        'aria-setsize': number
        role: 'listitem'
      }
    }) => (
      <EventRow
        index={index}
        style={style}
        ariaAttributes={ariaAttributes}
        events={events}
        onMeasured={handleRowMeasured}
        rowHeight={rowHeight}
      />
    ),
    [events, handleRowMeasured, rowHeight],
  )

  return (
    <div className="events-feed">
      <div className="events-header">
        <h3>All events</h3>
        <span>{total} total</span>
      </div>
      <div ref={listRef} className="event-list feed-list">
        {events.length > 0 && listHeight > 0 && listSize.width > 0 && (
          <List
            style={{ height: listHeight, width: listSize.width }}
            rowCount={events.length}
            rowHeight={rowHeight}
            rowComponent={rowComponent}
            rowProps={{}}
            onRowsRendered={({ stopIndex }) => {
              handleItemsRendered({ stopIndex })
            }}
            overscanCount={4}
          />
        )}
        {page.loading && <div className="loading loading-overlay">Loading more events…</div>}
      </div>
      {error && (
        <div className="error-state">
          <p>{error}</p>
          <button className="retry-button" type="button" onClick={onRetry}>
            Retry
          </button>
        </div>
      )}
      {page.done && !error && events.length === 0 && (
        <div className="empty">No events recorded yet.</div>
      )}
    </div>
  )
}

interface AllAlertsProps {
  alerts: AlertItem[]
  total: number
  page: PageState
  onLoadMore: () => Promise<void>
  error: string | null
  onRetry: () => void
}

function AllAlertsFeed({ alerts, total, page, onLoadMore, error, onRetry }: AllAlertsProps) {
  const listRef = useRef<HTMLDivElement | null>(null)
  const rowHeight = useDynamicRowHeight({ defaultRowHeight: 136 })
  const [listSize, setListSize] = useState({ width: 0, maxHeight: 0 })
  const handleItemsRendered = useCallback(
    ({ stopIndex }: { stopIndex: number }) => {
      if (page.loading || page.done || error) return
      if (stopIndex >= alerts.length - 4) {
        void onLoadMore()
      }
    },
    [alerts.length, onLoadMore, page.done, page.loading, error],
  )

  const handleRowMeasured = useCallback(() => {
    setListSize((prev) => ({ ...prev }))
  }, [])

  useLayoutEffect(() => {
    const element = listRef.current
    if (!element) return undefined

    const updateSize = () => {
      const listRect = element.getBoundingClientRect()
      const sectionElement = element.closest('.container-list')
      const sectionStyle = sectionElement ? window.getComputedStyle(sectionElement) : null
      const sectionPaddingBottom = sectionStyle
        ? Number.parseFloat(sectionStyle.paddingBottom) || 0
        : 0
      const viewportHeight = document.documentElement.clientHeight
      const maxHeight = Math.max(200, viewportHeight - listRect.top - sectionPaddingBottom - 1)
      const width = listRect.width
      if (!maxHeight || !width) return
      setListSize({ width, maxHeight })
    }

    updateSize()
    const observer = new ResizeObserver(updateSize)
    observer.observe(element)
    window.addEventListener('resize', updateSize)

    return () => {
      observer.disconnect()
      window.removeEventListener('resize', updateSize)
    }
  }, [])

  const listHeight = useMemo(() => {
    if (alerts.length === 0 || listSize.maxHeight === 0) return 0
    const average = rowHeight.getAverageRowHeight() || 136
    let total = 0
    for (let index = 0; index < alerts.length; index += 1) {
      total += rowHeight.getRowHeight(index) ?? average
      if (total >= listSize.maxHeight) {
        return listSize.maxHeight
      }
    }
    return Math.min(total, listSize.maxHeight)
  }, [alerts.length, listSize.maxHeight, rowHeight])

  const rowComponent = useCallback(
    ({
      index,
      style,
      ariaAttributes,
    }: {
      index: number
      style: CSSProperties
      ariaAttributes: {
        'aria-posinset': number
        'aria-setsize': number
        role: 'listitem'
      }
    }) => (
      <AlertRow
        index={index}
        style={style}
        ariaAttributes={ariaAttributes}
        alerts={alerts}
        onMeasured={handleRowMeasured}
        rowHeight={rowHeight}
      />
    ),
    [alerts, handleRowMeasured, rowHeight],
  )

  return (
    <div className="events-feed">
      <div className="events-header">
        <h3>All alerts</h3>
        <span>{total} total</span>
      </div>
      <div ref={listRef} className="event-list feed-list">
        {alerts.length > 0 && listHeight > 0 && listSize.width > 0 && (
          <List
            style={{ height: listHeight, width: listSize.width }}
            rowCount={alerts.length}
            rowHeight={rowHeight}
            rowComponent={rowComponent}
            rowProps={{}}
            onRowsRendered={({ stopIndex }) => {
              handleItemsRendered({ stopIndex })
            }}
            overscanCount={4}
          />
        )}
        {page.loading && <div className="loading loading-overlay">Loading more alerts…</div>}
      </div>
      {error && (
        <div className="error-state">
          <p>{error}</p>
          <button className="retry-button" type="button" onClick={onRetry}>
            Retry
          </button>
        </div>
      )}
      {page.done && !error && alerts.length === 0 && (
        <div className="empty">No alerts recorded yet.</div>
      )}
    </div>
  )
}

interface EventRowProps {
  index: number
  style: CSSProperties
  ariaAttributes: {
    'aria-posinset': number
    'aria-setsize': number
    role: 'listitem'
  }
  events: EventItem[]
  onMeasured: () => void
  rowHeight: ReturnType<typeof useDynamicRowHeight>
}

function EventRow({ index, style, ariaAttributes, events, onMeasured, rowHeight }: EventRowProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const notifiedRef = useRef(false)

  useLayoutEffect(() => {
    if (!ref.current) return undefined
    const cleanup = rowHeight.observeRowElements([ref.current])
    if (!notifiedRef.current) {
      notifiedRef.current = true
      onMeasured()
    }
    return cleanup
  }, [rowHeight, onMeasured])

  const event = events[index]
  const isLast = index === events.length - 1
  const title = deriveEventTitle(event)
  const changeLine = deriveChangeLine(event)
  return (
    <div
      ref={ref}
      role={ariaAttributes.role}
      aria-posinset={ariaAttributes['aria-posinset']}
      aria-setsize={ariaAttributes['aria-setsize']}
      style={style}
      className={`event-row feed-row ${index % 2 === 0 ? 'feed-row-even' : 'feed-row-odd'} ${
        isLast ? 'feed-row-last' : ''
      }`}
    >
      <div className={`event-dot ${eventSeverityClass(event)}`} />
      <div className="event-body">
        <div className="event-top">
          <span className="event-title">{title}</span>
          <span className="event-time">{formatDate(event.timestamp)}</span>
        </div>
        <div className="event-identity">
          <span className="event-container">{event.container}</span>
          {event.container_id && (
            <span className="event-id" title={event.container_id}>
              ({shortId(event.container_id)})
            </span>
          )}
        </div>
        {changeLine && <div className="event-change">{changeLine}</div>}
        {event.exit_code != null && <div className="event-meta">Exit code: {event.exit_code}</div>}
      </div>
    </div>
  )
}

interface AlertRowProps {
  index: number
  style: CSSProperties
  ariaAttributes: {
    'aria-posinset': number
    'aria-setsize': number
    role: 'listitem'
  }
  alerts: AlertItem[]
  onMeasured: () => void
  rowHeight: ReturnType<typeof useDynamicRowHeight>
}

function AlertRow({ index, style, ariaAttributes, alerts, onMeasured, rowHeight }: AlertRowProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const notifiedRef = useRef(false)

  useLayoutEffect(() => {
    if (!ref.current) return undefined
    const cleanup = rowHeight.observeRowElements([ref.current])
    if (!notifiedRef.current) {
      notifiedRef.current = true
      onMeasured()
    }
    return cleanup
  }, [rowHeight, onMeasured])

  const alert = alerts[index]
  const isLast = index === alerts.length - 1
  const title = deriveAlertTitle(alert)
  const changeLine = deriveAlertChangeLine(alert)
  let message = alert.message
  if (alert.type === 'restart_loop') {
    const count = parseAlertRestartCount(alert)
    message = count ? `Restart loop detected (${String(count)} restarts)` : 'Restart loop detected'
  }
  return (
    <div
      ref={ref}
      role={ariaAttributes.role}
      aria-posinset={ariaAttributes['aria-posinset']}
      aria-setsize={ariaAttributes['aria-setsize']}
      style={style}
      className={`event-row feed-row ${index % 2 === 0 ? 'feed-row-even' : 'feed-row-odd'} ${
        isLast ? 'feed-row-last' : ''
      }`}
    >
      <div className={`event-dot ${alertSeverityClass(alert)}`} />
      <div className="event-body">
        <div className="event-top">
          <span className="event-title">{title}</span>
          <span className="event-time">{formatDate(alert.timestamp)}</span>
        </div>
        <div className="event-message">{message}</div>
        <div className="event-identity">{deriveAlertContainerLine(alert)}</div>
        {changeLine && <div className="event-change">{changeLine}</div>}
        {alert.exit_code != null && <div className="event-meta">Exit code: {alert.exit_code}</div>}
      </div>
    </div>
  )
}
