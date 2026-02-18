import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import './App.css'

interface Container {
  id: number
  name: string
  container_id: string
  image: string
  image_tag: string
  image_id: string
  created_at: string
  first_seen_at: string
  status: string
  caps: string[]
  read_only: boolean
  user: string
  last_event: EventItem | null
}

interface EventItem {
  id: number
  container_pk: number
  container: string
  container_id: string
  type: string
  severity: string
  message: string
  timestamp: string
  old_image: string
  new_image: string
  old_image_id: string
  new_image_id: string
  reason: string
  details: string
}

interface EventUpdate {
  container: Container
  event: EventItem
}

interface PageState {
  beforeId?: number
  loading: boolean
  done: boolean
}

const PAGE_SIZE = 20

const statusClass = (status: string) => {
  const s = status.toLowerCase()
  if (s === 'running') return 'status-running'
  if (s === 'crashed' || s === 'oom') return 'status-error'
  if (s === 'exited' || s === 'dead') return 'status-down'
  return 'status-warn'
}

const severityClass = (severity: string) => {
  const s = severity.toLowerCase()
  if (s === 'red') return 'sev-red'
  if (s === 'green') return 'sev-green'
  return 'sev-blue'
}

const formatDate = (val: string) => {
  if (!val) return '—'
  return new Date(val).toLocaleString()
}

export default function App() {
  const [containers, setContainers] = useState<Container[]>([])
  const [expanded, setExpanded] = useState<Record<string, boolean | undefined>>({})
  const [events, setEvents] = useState<Record<string, EventItem[] | undefined>>({})
  const [pages, setPages] = useState<Record<string, PageState | undefined>>({})
  const [flash, setFlash] = useState<Record<string, boolean | undefined>>({})

  const sortedContainers = useMemo(() => {
    return [...containers].sort((a, b) => a.name.localeCompare(b.name))
  }, [containers])

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
      const data = (await res.json()) as EventItem[]
      setEvents((prev) => ({
        ...prev,
        [name]: [...(prev[name] ?? []), ...data],
      }))
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
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial load on mount
    void loadContainers()
  }, [loadContainers])

  useEffect(() => {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${wsProtocol}://${window.location.host}/api/events/stream`)

    ws.onmessage = (event) => {
      if (typeof event.data !== 'string') return
      const update = JSON.parse(event.data) as EventUpdate
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

      setEvents((prev) => {
        if (!(expanded[update.container.name] ?? false)) return prev
        const current = prev[update.container.name] ?? []
        if (current.some((item) => item.id === update.event.id)) return prev
        return {
          ...prev,
          [update.container.name]: [update.event, ...current],
        }
      })
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
          <span className="tag">healthmon</span>
          <h1>Container Health Monitor</h1>
        </div>
        <button className="refresh" type="button" onClick={handleRefresh}>
          Refresh
        </button>
      </header>

      <section className="container-list">
        {sortedContainers.length === 0 && <div className="empty">No containers found yet.</div>}
        {sortedContainers.map((container) => (
          <ContainerRow
            key={container.name}
            container={container}
            expanded={expanded[container.name] ?? false}
            flash={flash[container.name] ?? false}
            onToggle={toggleExpanded}
            events={events[container.name] ?? []}
            page={pages[container.name] ?? { loading: false, done: false }}
            onLoadMore={loadEvents}
          />
        ))}
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
  page: PageState
  onLoadMore: (name: string) => Promise<void>
}

function ContainerRow({
  container,
  expanded,
  flash,
  onToggle,
  events,
  page,
  onLoadMore,
}: RowProps) {
  const sentinelRef = useRef<HTMLDivElement | null>(null)

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

  return (
    <article className={`container-card ${expanded ? 'expanded' : ''} ${flash ? 'flash' : ''}`}>
      <button
        className="container-main"
        type="button"
        onClick={() => {
          onToggle(container.name)
        }}
      >
        <div className={`status-dot ${statusClass(container.status)}`} />
        <div className="container-info">
          <div className="name-row">
            <span className="name">{container.name}</span>
            <span className="status-pill">{container.status}</span>
          </div>
          <div className="meta">
            <span>
              {container.image}:{container.image_tag}
            </span>
            <span>Created: {formatDate(container.created_at)}</span>
            <span>First seen: {formatDate(container.first_seen_at)}</span>
          </div>
        </div>
        <div className="last-event">
          {container.last_event ? (
            <div className="event-summary">
              <span className={`event-dot ${severityClass(container.last_event.severity)}`} />
              <div>
                <div className="event-type">{container.last_event.type}</div>
                <div className="event-message">{container.last_event.message}</div>
              </div>
            </div>
          ) : (
            <div className="event-summary empty">No events yet</div>
          )}
        </div>
      </button>

      {expanded && (
        <div className="container-details">
          <div className="details-grid">
            <div>
              <h3>Runtime</h3>
              <p>User: {container.user}</p>
              <p>Read-only: {container.read_only ? 'yes' : 'no'}</p>
              <p>
                Image ID: <span className="truncate-id">{container.image_id}</span>
              </p>
              <p>
                Container ID: <span className="truncate-id">{container.container_id}</span>
              </p>
            </div>
            <div>
              <h3>Capabilities</h3>
              <p className="caps">{container.caps.length ? container.caps.join(', ') : 'none'}</p>
            </div>
          </div>

          <div className="events">
            <div className="events-header">
              <h3>Events</h3>
              <span>{events.length} loaded</span>
            </div>
            <div className="event-list">
              {events.map((event) => (
                <div key={event.id} className="event-row">
                  <div className={`event-dot ${severityClass(event.severity)}`} />
                  <div className="event-body">
                    <div className="event-top">
                      <span className="event-type">{event.type}</span>
                      <span className="event-time">{formatDate(event.timestamp)}</span>
                    </div>
                    <div className="event-message">{event.message}</div>
                    {event.reason && <div className="event-reason">Reason: {event.reason}</div>}
                    {(event.old_image || event.new_image) && (
                      <div className="event-change">
                        {event.old_image} → {event.new_image}
                      </div>
                    )}
                  </div>
                </div>
              ))}
              {!page.done && <div ref={sentinelRef} className="event-sentinel" />}
            </div>
            {page.loading && <div className="loading">Loading more events…</div>}
            {page.done && events.length === 0 && (
              <div className="empty">No events recorded yet.</div>
            )}
          </div>
        </div>
      )}
    </article>
  )
}
