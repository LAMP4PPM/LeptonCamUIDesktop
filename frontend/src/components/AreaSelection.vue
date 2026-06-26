<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { useDeviceState } from '../composables/useDeviceState'

const { frame, regions, activeRegionCount, clearRegions } =
    useDeviceState()

const canvas = ref<HTMLCanvasElement | null>(null)

/* Grayscale thumbnail of the same frame, so regions read clearly on top. */
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
        const v = Math.round(((f.temps[i] - f.min) / range) * 200) + 20
        const o = i * 4
        img.data[o] = v
        img.data[o + 1] = v
        img.data[o + 2] = v
        img.data[o + 3] = 255
    }
    ctx.putImageData(img, 0, 0)
}

watch(frame, render, { deep: false })
onMounted(render)
</script>

<template>
    <section class="rounded-2xl border border-[#1b2530] bg-[#0e141a] p-4">
        <h2 class="flex items-center gap-2 font-mono text-[11px] tracking-[0.2em] uppercase text-[#8a99a6]">
            <svg viewBox="0 0 24 24" class="h-4 w-4 text-[#5f6e7b]" fill="none" stroke="currentColor" stroke-width="2"
                stroke-linecap="round">
                <path
                    d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3" />
            </svg>
            Area Selection
        </h2>

        <!-- Mini map -->
        <div class="relative mt-3 overflow-hidden rounded-lg border border-[#1b2530] bg-black aspect-[16/9]">
            <canvas ref="canvas" class="absolute inset-0 h-full w-full opacity-80" style="image-rendering: pixelated" />

            <!-- Area ROIs: true-sized box, relative to the mini map itself -->
            <div v-for="r in regions" :key="`box-${r.id}`" v-show="r.w || r.h"
                class="pointer-events-none absolute rounded-sm border bg-white/10" :style="{
                    left: `${(r.x - r.w / 2) * 100}%`,
                    top: `${(r.y - r.h / 2) * 100}%`,
                    width: `${r.w * 100}%`,
                    height: `${r.h * 100}%`,
                    borderColor: r.color,
                }" />

            <!-- Point ROIs: small fixed marker (no intrinsic size to draw) -->
            <div v-for="r in regions" :key="r.id" v-show="!r.w && !r.h"
                class="pointer-events-none absolute -translate-x-1/2 -translate-y-1/2"
                :style="{ left: `${r.x * 100}%`, top: `${r.y * 100}%` }">
                <div class="relative h-7 w-9 rounded-sm border" :style="{ borderColor: r.color }">
                    <span class="absolute -top-3 left-0 font-mono text-[7px] tracking-wider"
                        :style="{ color: r.color }">{{ r.id }}</span>
                </div>
            </div>
        </div>

        <!-- Active region count -->
        <div class="mt-3 flex items-center justify-between font-mono text-[11px] tracking-[0.15em] uppercase">
            <span class="text-[#7c8b98]">Active Regions</span>
            <span class="text-[#c2cdd6] tabular-nums">{{ activeRegionCount }}</span>
        </div>

        <!-- Controls -->
        <div class="mt-3 grid grid-cols-1">
            <button class="rounded-lg border border-[#27333f] bg-[#0b1116] py-2 font-mono text-[11px] tracking-[0.15em] uppercase
               text-[#c2cdd6] hover:border-[#f59e0b]/60 hover:text-[#f59e0b] transition-colors"
                @click="clearRegions()">
                Clear All
            </button>
        </div>
    </section>
</template>