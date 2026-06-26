<script setup lang="ts">
import { ref, watch, onMounted, computed } from 'vue'
import { useDeviceState } from '../composables/useDeviceState'

const {
    frame,
    centerTemp,
    streaming,
    regions,
    fps,
    iso,
    fStop,
    toggleStream,
    defineRegion,
} = useDeviceState()

const canvas = ref<HTMLCanvasElement | null>(null)
const stage = ref<HTMLElement | null>(null)

/* INFERNO colormap (matplotlib), 11 anchor stops, linearly interpolated. */
const INFERNO: [number, number, number][] = [
    [0, 0, 4],
    [22, 11, 57],
    [62, 15, 114],
    [103, 28, 117],
    [143, 41, 107],
    [183, 55, 84],
    [218, 76, 55],
    [243, 116, 27],
    [252, 165, 10],
    [246, 215, 70],
    [252, 255, 164],
]

function inferno(t: number): [number, number, number] {
    const x = Math.min(1, Math.max(0, t)) * (INFERNO.length - 1)
    const i = Math.floor(x)
    const f = x - i
    const a = INFERNO[i]
    const b = INFERNO[Math.min(INFERNO.length - 1, i + 1)]
    return [
        a[0] + (b[0] - a[0]) * f,
        a[1] + (b[1] - a[1]) * f,
        a[2] + (b[2] - a[2]) * f,
    ]
}

function render() {
    const cv = canvas.value
    const f = frame.value
    if (!cv || !f.temps.length) return
    cv.width = f.width
    cv.height = f.height
    const ctx = cv.getContext('2d')
    if (!ctx) return

    const img = ctx.createImageData(f.width, f.height)
    const range = f.max - f.min || 1
    for (let i = 0; i < f.temps.length; i++) {
        const norm = (f.temps[i] - f.min) / range
        const [r, g, b] = inferno(norm)
        const o = i * 4
        img.data[o] = r
        img.data[o + 1] = g
        img.data[o + 2] = b
        img.data[o + 3] = 255
    }
    ctx.putImageData(img, 0, 0)
}

watch(frame, render, { deep: false })
onMounted(render)

/* -------------------------------------------------------------------------- */
/* Click for a point ROI, drag for an area ROI                                */
/* -------------------------------------------------------------------------- */

const dragStart = ref<{ x: number; y: number } | null>(null)
const dragCurrent = ref<{ x: number; y: number } | null>(null)

// below this, a drag is treated as a click (a point ROI instead of an area)
const DRAG_THRESHOLD = 0.012

function normalizedPoint(e: PointerEvent) {
    const el = stage.value
    if (!el) return { x: 0, y: 0 }
    const rect = el.getBoundingClientRect()
    return {
        x: Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)),
        y: Math.min(1, Math.max(0, (e.clientY - rect.top) / rect.height)),
    }
}

function onPointerDown(e: PointerEvent) {
    dragStart.value = normalizedPoint(e)
    dragCurrent.value = dragStart.value
        ; (e.currentTarget as HTMLElement)?.setPointerCapture?.(e.pointerId)
}

function onPointerMove(e: PointerEvent) {
    if (!dragStart.value) return
    dragCurrent.value = normalizedPoint(e)
}

async function onPointerUp(e: PointerEvent) {
    if (!dragStart.value || !dragCurrent.value) return
    const start = dragStart.value
    const end = dragCurrent.value
    dragStart.value = null
    dragCurrent.value = null

    const dx = Math.abs(end.x - start.x)
    const dy = Math.abs(end.y - start.y)

    if (dx < DRAG_THRESHOLD && dy < DRAG_THRESHOLD) {
        await defineRegion({ x: end.x, y: end.y })
    } else {
        await defineRegion({
            x: (start.x + end.x) / 2,
            y: (start.y + end.y) / 2,
            w: dx,
            h: dy,
        })
    }
}

/** Live rectangle shown while dragging, as section-relative percentages. */
const previewBox = computed(() => {
    if (!dragStart.value || !dragCurrent.value) return null
    const x0 = Math.min(dragStart.value.x, dragCurrent.value.x)
    const x1 = Math.max(dragStart.value.x, dragCurrent.value.x)
    const y0 = Math.min(dragStart.value.y, dragCurrent.value.y)
    const y1 = Math.max(dragStart.value.y, dragCurrent.value.y)
    return {
        left: x0 * 100,
        top: y0 * 100,
        width: (x1 - x0) * 100,
        height: (y1 - y0) * 100,
    }
})
</script>

<template>
    <section class="relative overflow-hidden rounded-2xl border border-[#1b2530] bg-black aspect-[4/3]">
        <!-- Thermal feed (80x60 upscaled, nearest-neighbour like the sensor) -->
        <canvas ref="canvas" class="absolute inset-0 h-full w-full" style="image-rendering: pixelated" />

        <!-- Click = point ROI, drag = area ROI (averaged on read + on recording) -->
        <div ref="stage" class="absolute inset-0 cursor-crosshair" style="touch-action: none"
            @pointerdown="onPointerDown" @pointermove="onPointerMove" @pointerup="onPointerUp"
            @pointerleave="onPointerUp" />

        <!-- Live drag preview -->
        <div v-if="previewBox"
            class="pointer-events-none absolute rounded-sm border-2 border-dashed border-white/80 bg-white/10" :style="{
                left: `${previewBox.left}%`,
                top: `${previewBox.top}%`,
                width: `${previewBox.width}%`,
                height: `${previewBox.height}%`,
            }" />

        <!-- Dim vignette so overlays stay readable -->
        <div class="pointer-events-none absolute inset-0 bg-gradient-to-b from-black/30 via-transparent to-black/40" />

        <!-- Top-left: feed label + live temperature -->
        <div class="absolute left-4 top-4 space-y-1">
            <div class="flex items-center gap-2 font-mono text-[10px] tracking-[0.2em] uppercase text-[#c8d2da]">
                <span class="h-1.5 w-1.5 rounded-full"
                    :class="streaming ? 'bg-[#2dd4bf] shadow-[0_0_8px_#2dd4bf] animate-pulse' : 'bg-[#5f6e7b]'" />
                Live Stream • Primary Feed
            </div>
            <div class="font-mono text-3xl font-semibold text-white tabular-nums drop-shadow">
                {{ centerTemp.toFixed(1) }}°C
            </div>
            <div class="font-mono text-[9px] tracking-[0.12em] uppercase text-white/50">
                {{ 'Click for a point · Drag for an area' }}
            </div>
        </div>

        <!-- Top-right: stream controls -->
        <div class="absolute right-4 top-4 flex flex-col gap-2">
            <button class="grid place-items-center h-9 w-9 rounded-lg border border-[#2a3742] bg-black/40 backdrop-blur
               text-[#c8d2da] hover:text-[#2dd4bf] hover:border-[#2dd4bf]/50 transition-colors"
                :aria-label="streaming ? 'Pause stream' : 'Resume stream'" @click="toggleStream()">
                <svg viewBox="0 0 24 24" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="2"
                    stroke-linecap="round" stroke-linejoin="round">
                    <rect x="2" y="6" width="14" height="12" rx="2" />
                    <path d="m16 10 6-3.5v11L16 14" />
                    <path v-if="!streaming" d="M3 3l18 18" stroke="#f59e0b" />
                </svg>
            </button>
        </div>

        <!-- Center crosshair (matches the Python center marker) -->
        <div class="pointer-events-none absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2">
            <div class="relative h-7 w-7">
                <span class="absolute left-0 top-0 h-2 w-2 border-l-2 border-t-2 border-white/80" />
                <span class="absolute right-0 top-0 h-2 w-2 border-r-2 border-t-2 border-white/80" />
                <span class="absolute bottom-0 left-0 h-2 w-2 border-b-2 border-l-2 border-white/80" />
                <span class="absolute bottom-0 right-0 h-2 w-2 border-b-2 border-r-2 border-white/80" />
            </div>
        </div>

        <!-- Area ROI boxes (sized + positioned relative to the feed itself, not
         the centered marker below, so percentages mean what they say) -->
        <div v-for="r in regions" :key="`box-${r.id}`" v-show="r.w || r.h"
            class="pointer-events-none absolute rounded-sm border bg-white/5" :style="{
                left: `${(r.x - r.w / 2) * 100}%`,
                top: `${(r.y - r.h / 2) * 100}%`,
                width: `${r.w * 100}%`,
                height: `${r.h * 100}%`,
                borderColor: r.color,
            }" />

        <!-- Region markers: label + temp for every ROI, circle only for points -->
        <div v-for="r in regions" :key="r.id" class="pointer-events-none absolute -translate-x-1/2 -translate-y-1/2"
            :style="{ left: `${r.x * 100}%`, top: `${r.y * 100}%` }">
            <div class="flex flex-col items-center gap-1">
                <span class="font-mono text-[9px] tracking-[0.15em] uppercase" :style="{ color: r.color }">{{ r.id
                }}</span>
                <div v-if="!r.w && !r.h" class="relative grid place-items-center h-6 w-6 rounded-full border"
                    :style="{ borderColor: r.color }">
                    <span class="absolute h-3 w-px" :style="{ background: r.color }" />
                    <span class="absolute h-px w-3" :style="{ background: r.color }" />
                </div>
                <span class="font-mono text-[9px] text-white/90 tabular-nums">{{ r.temp.toFixed(1) }}°</span>
            </div>
        </div>

        <!-- Bottom-left: capture metadata -->
        <div class="absolute bottom-4 left-4 font-mono text-[10px] tracking-[0.18em] uppercase text-[#9aa7b2]">
            ISO: {{ iso }} | F: {{ fStop.toFixed(1) }} | FR: {{ fps }}FPS
        </div>
    </section>
</template>