import { ref, computed, readonly } from 'vue'

/**
 * useDeviceState
 * --------------------------------------------------------------------------
 * Single source of truth between the Go (Wails) backend and the Vue UI for the
 * AERO-THERM thermal sensor app.
 *
 * The Go backend is expected to:
 *   1. Emit a `device:frame` event for every decoded thermal frame.
 *   2. Emit a `device:status` event when the connection / device changes.
 *   3. Emit a `ports:list` event periodically with the available serial ports.
 *   4. Expose bound methods on `window.go.main.App.*` for commands
 *      (connect/disconnect serial, start/stop stream, recording, capture
 *      rate, regions).
 *
 * Frame contract (mirrors the reference Python: 80x60 uint16 -> Celsius):
 *
 *   interface DeviceFrame {
 *     width:  number          // 80
 *     height: number          // 60
 *     temps:  number[]        // length width*height, row-major, degrees C
 *     min:    number          // min temp in frame
 *     max:    number          // max temp in frame
 *     center: number          // temp at the center pixel
 *     fps?:   number          // optional reported frame rate
 *     ts?:    number          // optional capture timestamp (ms)
 *   }
 *
 * If no Wails runtime is present (e.g. plain `vite dev`), the composable
 * generates a synthetic moving-hotspot frame so the UI is fully alive while
 * the Go backend is still being built.
 * --------------------------------------------------------------------------
 */

export interface DeviceFrame {
    width: number
    height: number
    temps: number[]
    min: number
    max: number
    center: number
    fps?: number
    ts?: number
}

export interface Region {
    id: string
    /** normalized center position in the frame, 0..1 */
    x: number
    y: number
    /**
     * normalized box size, 0..1. `0` on both axes means this ROI is a
     * single-pixel point sample; non-zero means `temp` is the average over
     * that box (sampleRegionValue below, and Go's sampleRegion, both branch
     * on this so the live readout and recorded CSV value always agree).
     */
    w: number
    h: number
    /** latest sampled temperature, degrees C — a single pixel or a box average */
    temp: number
    color: string
}

/* -------------------------------------------------------------------------- */
/* Wails bridge (typed, optional)                                             */
/* -------------------------------------------------------------------------- */

type WailsBackend = {
    ConnectSerial?: (name: string) => Promise<void>
    DisconnectSerial?: () => Promise<void>
    GetConnectedPort?: () => Promise<string>
    StartStream?: () => Promise<void>
    StopStream?: () => Promise<void>
    StartRecording?: () => Promise<void>
    StopRecording?: () => Promise<void>
    SetCaptureRate?: (ms: number) => Promise<void>
    DefineRegion?: (id: string, x: number, y: number, w: number, h: number) => Promise<void>
    ClearRegions?: () => Promise<void>
}

declare global {
    interface Window {
        runtime?: {
            EventsOn: (event: string, cb: (...data: any[]) => void) => () => void
            EventsEmit?: (event: string, ...data: any[]) => void
        }
        go?: { main?: { App?: WailsBackend } }
    }
}

const backend = (): WailsBackend | undefined => window.go?.main?.App
const hasRuntime = (): boolean => typeof window !== 'undefined' && !!window.runtime

/** Call a backend method if it exists; never throws when the binding is absent. */
async function call<K extends keyof WailsBackend>(
    method: K,
    ...args: Parameters<NonNullable<WailsBackend[K]>>
): Promise<void> {
    const fn = backend()?.[method] as ((...a: any[]) => Promise<void>) | undefined
    if (fn) {
        try {
            await fn(...args)
        } catch (err) {
            console.error(`[useDeviceState] backend.${String(method)} failed`, err)
        }
    }
}

function clamp(v: number, lo: number, hi: number) {
    return Math.min(hi, Math.max(lo, v))
}

/* -------------------------------------------------------------------------- */
/* Constants                                                                  */
/* -------------------------------------------------------------------------- */

const FRAME_W = 80
const FRAME_H = 60
const HISTORY_LEN = 160
const REGION_COLORS = ['#2dd4bf', '#f59e0b', '#a78bfa', '#38bdf8']

/* -------------------------------------------------------------------------- */
/* Module-level singleton state (shared by every component)                   */
/* -------------------------------------------------------------------------- */

const connection = ref<'connected' | 'connecting' | 'disconnected'>('disconnected')
const systemId = ref('104.22.8')
const startedAt = ref<number>(Date.now())
const now = ref<number>(Date.now())

/** True once we know whether a real Wails runtime is present. */
const backendAvailable = ref(false)
/** Serial ports reported by the backend's port watcher (`ports:list`). */
const availablePorts = ref<string[]>([])
/** Port currently being dialed, while we wait for ConnectSerial to settle. */
const connectingPort = ref<string | null>(null)

const streaming = ref(false)
const frame = ref<DeviceFrame>({
    width: FRAME_W,
    height: FRAME_H,
    temps: new Array(FRAME_W * FRAME_H).fill(0),
    min: 0,
    max: 0,
    center: 0,
})
const fps = ref(0)
const iso = ref(800)
const fStop = ref(2.8)

const regions = ref<Region[]>([
])

const recording = ref(false)
const recordingMs = ref(0)
const storageBytes = ref(0)
const captureRate = ref(10) // sec


const autoScale = ref(true)
const samplingMs = ref(10)
const currentMax = ref(0)
const highestRecorded = ref(0)

/** Per-region temperature history for the fluctuations chart. */
const history = ref<Record<string, number[]>>({})

/* -------------------------------------------------------------------------- */
/* Derived state                                                              */
/* -------------------------------------------------------------------------- */

const centerTemp = computed(() => frame.value.center)
const activeRegionCount = computed(() => regions.value.length)

/** systemId only reflects a real port name once we're actually connected. */
const connectedPort = computed(() => (connection.value === 'connected' ? systemId.value : ''))

const uptimeLabel = computed(() => {
    const total = Math.max(0, now.value - startedAt.value)
    const h = Math.floor(total / 3_600_000)
    const m = Math.floor((total % 3_600_000) / 60_000)
    return `${h}h ${String(m).padStart(2, '0')}m`
})

const recordingLabel = computed(() => {
    const s = Math.floor(recordingMs.value / 1000)
    const hh = String(Math.floor(s / 3600)).padStart(2, '0')
    const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
    const ss = String(s % 60).padStart(2, '0')
    return `${hh}:${mm}:${ss}`
})

const storageLabel = computed(() => {
    const gb = storageBytes.value / 1_073_741_824
    if (gb >= 1) return `${gb.toFixed(1)}GB`
    return `${(storageBytes.value / 1_048_576).toFixed(0)}MB`
})

/* -------------------------------------------------------------------------- */
/* Frame ingestion                                                            */
/* -------------------------------------------------------------------------- */

/**
 * Sample one region against a frame: a single pixel for a point ROI
 * (w === 0 && h === 0), or the mean of every pixel inside the box for an
 * area ROI. Mirrors Go's sampleRegion exactly so the live readout and the
 * recorded CSV value always agree.
 */
function sampleRegionValue(f: DeviceFrame, r: Region): number {
    if (!r.w && !r.h) {
        const px = clamp(Math.round(r.x * (f.width - 1)), 0, f.width - 1)
        const py = clamp(Math.round(r.y * (f.height - 1)), 0, f.height - 1)
        return f.temps[py * f.width + px] ?? r.temp
    }

    let x0 = Math.round((r.x - r.w / 2) * (f.width - 1))
    let x1 = Math.round((r.x + r.w / 2) * (f.width - 1))
    let y0 = Math.round((r.y - r.h / 2) * (f.height - 1))
    let y1 = Math.round((r.y + r.h / 2) * (f.height - 1))
    x0 = clamp(x0, 0, f.width - 1)
    x1 = clamp(x1, 0, f.width - 1)
    y0 = clamp(y0, 0, f.height - 1)
    y1 = clamp(y1, 0, f.height - 1)
    if (x1 < x0) [x0, x1] = [x1, x0]
    if (y1 < y0) [y0, y1] = [y1, y0]

    let sum = 0
    let n = 0
    for (let y = y0; y <= y1; y++) {
        const row = y * f.width
        for (let x = x0; x <= x1; x++) {
            sum += f.temps[row + x]
            n++
        }
    }
    return n ? sum / n : r.temp
}

function sampleRegions(f: DeviceFrame) {
    for (const r of regions.value) {
        r.temp = sampleRegionValue(f, r)

        const series = history.value[r.id] ?? []
        series.push(r.temp)
        if (series.length > HISTORY_LEN) series.shift()
        history.value[r.id] = series
    }
    // trigger reactivity on the history object
    history.value = { ...history.value }
}

function ingestFrame(f: DeviceFrame) {
    frame.value = f
    fps.value = f.fps ?? fps.value
    currentMax.value = f.max
    if (f.max > highestRecorded.value) highestRecorded.value = f.max
    sampleRegions(f)
}

/* -------------------------------------------------------------------------- */
/* Mock device (used only when the Wails runtime is unavailable)              */
/* -------------------------------------------------------------------------- */

let mockTimer: number | undefined
let mockPhase = 0

function buildMockFrame(): DeviceFrame {
    const temps = new Array(FRAME_W * FRAME_H)
    mockPhase += 0.05
    // a hot spot that drifts across the sensor
    const hx = (Math.sin(mockPhase) * 0.5 + 0.5) * FRAME_W
    const hy = (Math.cos(mockPhase * 0.7) * 0.5 + 0.5) * FRAME_H
    let min = Infinity
    let max = -Infinity
    for (let y = 0; y < FRAME_H; y++) {
        for (let x = 0; x < FRAME_W; x++) {
            const d2 = (x - hx) ** 2 + (y - hy) ** 2
            const blob = 55 * Math.exp(-d2 / 120)
            const ripple = 4 * Math.sin(x * 0.25 + mockPhase * 2)
            const t = 28 + blob + ripple + (Math.random() * 2 - 1)
            temps[y * FRAME_W + x] = t
            if (t < min) min = t
            if (t > max) max = t
        }
    }
    return {
        width: FRAME_W,
        height: FRAME_H,
        temps,
        min,
        max,
        center: temps[Math.floor(FRAME_H / 2) * FRAME_W + Math.floor(FRAME_W / 2)],
        fps: 60,
        ts: Date.now(),
    }
}

function startMock() {
    stopMock()
    mockTimer = window.setInterval(() => {
        if (streaming.value) ingestFrame(buildMockFrame())
    }, 1000 / 30)
}

function stopMock() {
    if (mockTimer) {
        clearInterval(mockTimer)
        mockTimer = undefined
    }
}

/* -------------------------------------------------------------------------- */
/* Local clocks (uptime + recording timer + storage growth)                   */
/* -------------------------------------------------------------------------- */

let clockTimer: number | undefined

function startClock() {
    if (clockTimer) return
    let last = Date.now()
    clockTimer = window.setInterval(() => {
        const t = Date.now()
        const dt = t - last
        last = t
        now.value = t
        if (recording.value) {
            recordingMs.value += dt
            // rough storage accrual: ~3 MB per captured second
            storageBytes.value += (dt / 1000) * 3_000_000
        }
    }, 250)
}

/* -------------------------------------------------------------------------- */
/* Lifecycle / wiring                                                         */
/* -------------------------------------------------------------------------- */

let initialized = false
let unsubscribers: Array<() => void> = []

function connect() {
    if (initialized) return
    initialized = true
    startClock()

    backendAvailable.value = hasRuntime()

    if (backendAvailable.value) {
        connection.value = 'disconnected'
        unsubscribers.push(
            window.runtime!.EventsOn('device:frame', (payload: DeviceFrame) => {
                connection.value = 'connected'
                ingestFrame(payload)
            }),
        )
        unsubscribers.push(
            window.runtime!.EventsOn(
                'device:status',
                (status: { connected?: boolean; streaming?: boolean; systemId?: string }) => {
                    if (status.connected !== undefined)
                        connection.value = status.connected ? 'connected' : 'disconnected'
                    if (status.streaming !== undefined) streaming.value = status.streaming
                    if (status.systemId) systemId.value = status.systemId
                },
            ),
        )
        unsubscribers.push(
            window.runtime!.EventsOn('ports:list', (ports: string[]) => {
                availablePorts.value = Array.isArray(ports) ? ports : []
            }),
        )
    } else {
        // No backend yet — run the synthetic device so the UI is demonstrable.
        connection.value = 'connected'
        startMock()
    }

    // auto-start the stream
    void toggleStream(true)
}

function disconnect() {
    unsubscribers.forEach((u) => u())
    unsubscribers = []
    stopMock()
    initialized = false
    connection.value = 'disconnected'
    streaming.value = false
}

/* -------------------------------------------------------------------------- */
/* Commands                                                                   */
/* -------------------------------------------------------------------------- */

/**
 * Dial a serial port by name (e.g. "COM3" / "/dev/cu.usbserial-1410").
 * Goes through the backend directly (rather than the generic `call` helper)
 * so a failed Open() can drop the UI back to "disconnected" instead of
 * sticking on "connecting" forever — ConnectSerial returns its error before
 * emitting any device:status event, so there's nothing else to correct it.
 */
async function connectToPort(name: string) {
    const fn = backend()?.ConnectSerial
    if (!fn) return
    connectingPort.value = name
    connection.value = 'connecting'
    try {
        await fn(name)
        // success: the device:status listener above will confirm "connected"
    } catch (err) {
        console.error('[useDeviceState] ConnectSerial failed', err)
        connection.value = 'disconnected'
    } finally {
        connectingPort.value = null
    }
}

async function disconnectPort() {
    await call('DisconnectSerial')
}

async function toggleStream(force?: boolean) {
    const next = force ?? !streaming.value
    streaming.value = next
    await call(next ? 'StartStream' : 'StopStream')
}

async function toggleRecording(force?: boolean) {
    const next = force ?? !recording.value
    recording.value = next
    if (next) {
        recordingMs.value = 0
        storageBytes.value = 0
    }
    await call(next ? 'StartRecording' : 'StopRecording')
}

async function setCaptureRate(ms: number) {
    const clamped = Math.max(1, Math.round(ms) || 1)
    captureRate.value = clamped
    samplingMs.value = clamped
    await call('SetCaptureRate', clamped)
}

/**
 * Define a new ROI.
 *  - No args: auto-placed point ROI (legacy "Define New" button behavior).
 *  - { x, y }: a point ROI at that normalized position.
 *  - { x, y, w, h }: an area ROI — its recorded `temp` is the average over
 *    that normalized box, computed identically on the frontend
 *    (sampleRegionValue above) and in Go's sampleRegion, so the live
 *    readout always matches what lands in the CSV.
 */
async function defineRegion(opts?: { x?: number; y?: number; w?: number; h?: number }) {
    const idx = regions.value.length
    const id = `ROI_${String(idx + 1).padStart(2, '0')}`
    const region: Region = {
        id,
        x: opts?.x ?? 0.4 + idx * 0.08,
        y: opts?.y ?? 0.45,
        w: opts?.w ?? 0,
        h: opts?.h ?? 0,
        temp: 0,
        color: REGION_COLORS[idx % REGION_COLORS.length],
    }
    regions.value = [...regions.value, region]
    history.value = { ...history.value, [id]: [] }
    await call('DefineRegion', id, region.x, region.y, region.w, region.h)
}

async function clearRegions() {
    regions.value = []
    history.value = {}
    await call('ClearRegions')
}

function toggleAutoScale() {
    autoScale.value = !autoScale.value
}

/* -------------------------------------------------------------------------- */
/* Public hook                                                                */
/* -------------------------------------------------------------------------- */

export function useDeviceState() {
    return {
        // constants
        FRAME_W,
        FRAME_H,

        // connection / system
        connection: readonly(connection),
        systemId: readonly(systemId),
        connectedPort,
        uptimeLabel,
        backendAvailable: readonly(backendAvailable),
        availablePorts: readonly(availablePorts),
        connectingPort: readonly(connectingPort),

        // stream
        streaming: readonly(streaming),
        frame: readonly(frame),
        centerTemp,
        fps: readonly(fps),
        iso: readonly(iso),
        fStop: readonly(fStop),

        // regions
        regions: readonly(regions),
        activeRegionCount,

        // recording
        recording: readonly(recording),
        recordingLabel,
        storageLabel,
        captureRate: readonly(captureRate),

        // fluctuations
        autoScale: readonly(autoScale),
        samplingMs: readonly(samplingMs),
        currentMax: readonly(currentMax),
        highestRecorded: readonly(highestRecorded),
        history: readonly(history),

        // lifecycle
        connect,
        disconnect,

        // commands
        connectToPort,
        disconnectPort,
        toggleStream,
        toggleRecording,
        setCaptureRate,
        defineRegion,
        clearRegions,
        toggleAutoScale,
    }
}

export type DeviceState = ReturnType<typeof useDeviceState>