<script setup lang="ts">
import { computed } from 'vue'
import { useDeviceState } from '../composables/useDeviceState'

const {
  regions,
  history,
  autoScale,
  samplingMs,
  currentMax,
  highestRecorded,
  toggleAutoScale,
} = useDeviceState()

const VW = 1000
const VH = 220
const PAD = 8

/** y-range across all visible series (auto) or a fixed sensible band. */
const range = computed(() => {
  if (!autoScale.value) return { lo: 0, hi: 100 }
  let lo = Infinity
  let hi = -Infinity
  for (const r of regions.value) {
    for (const v of history.value[r.id] ?? []) {
      if (v < lo) lo = v
      if (v > hi) hi = v
    }
  }
  if (!Number.isFinite(lo) || !Number.isFinite(hi) || lo === hi) {
    return { lo: 20, hi: 60 }
  }
  const pad = (hi - lo) * 0.15
  return { lo: lo - pad, hi: hi + pad }
})

function pathFor(id: string): string {
  const data = history.value[id] ?? []
  if (data.length < 2) return ''
  const { lo, hi } = range.value
  const span = hi - lo || 1
  const stepX = (VW - PAD * 2) / (data.length - 1)
  return data
    .map((v, i) => {
      const x = PAD + i * stepX
      const y = PAD + (1 - (v - lo) / span) * (VH - PAD * 2)
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}
</script>

<template>
  <section class="rounded-2xl border border-[#1b2530] bg-[#0e141a] p-4">
    <!-- Header row -->
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-3">
        <h2 class="flex items-center gap-2 font-mono text-[11px] tracking-[0.2em] uppercase text-[#8a99a6]">
          <svg viewBox="0 0 24 24" class="h-4 w-4 text-[#5f6e7b]" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 3v18h18" />
            <path d="m6 14 4-5 3 3 5-7" />
          </svg>
          Real-Time Fluctuations
        </h2>

        <button
          class="rounded-md border px-2.5 py-1 font-mono text-[9px] tracking-[0.15em] uppercase transition-colors"
          :class="autoScale
            ? 'border-[#2dd4bf]/50 bg-[#2dd4bf]/10 text-[#2dd4bf]'
            : 'border-[#27333f] bg-[#0b1116] text-[#7c8b98] hover:text-[#c2cdd6]'"
          @click="toggleAutoScale()"
        >
          Auto-Scale {{ autoScale ? 'On' : 'Off' }}
        </button>

        <span class="font-mono text-[9px] tracking-[0.15em] uppercase text-[#5f6e7b]">
          Sampling: {{ samplingMs }}sec
        </span>
      </div>

      <!-- Readouts -->
      <div class="flex items-center gap-6">
        <div class="text-right">
          <div class="font-mono text-[9px] tracking-[0.15em] uppercase text-[#5f6e7b]">Current Max</div>
          <div class="font-mono text-lg font-semibold tabular-nums text-[#2dd4bf]">{{ currentMax.toFixed(1) }}°C</div>
        </div>
        <div class="text-right">
          <div class="font-mono text-[9px] tracking-[0.15em] uppercase text-[#5f6e7b]">Highest Recorded</div>
          <div class="font-mono text-lg font-semibold tabular-nums text-[#f59e0b]">{{ highestRecorded.toFixed(1) }}°C</div>
        </div>
      </div>
    </div>

    <!-- Chart -->
    <div class="relative mt-3">
      <svg :viewBox="`0 0 ${VW} ${VH}`" class="h-44 w-full" preserveAspectRatio="none">
        <!-- gridlines -->
        <g stroke="#1b2530" stroke-width="1">
          <line v-for="n in 4" :key="n" :x1="0" :x2="VW" :y1="(VH / 4) * n - VH / 8" :y2="(VH / 4) * n - VH / 8" />
        </g>
        <!-- series -->
        <path
          v-for="(r, i) in regions"
          :key="r.id"
          :d="pathFor(r.id)"
          fill="none"
          :stroke="r.color"
          stroke-width="2.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          :stroke-dasharray="i === 0 ? '' : '2 6'"
          vector-effect="non-scaling-stroke"
        />
      </svg>

      <!-- Legend -->
      <div class="absolute right-2 top-2 flex flex-col gap-1 rounded-md border border-[#1b2530] bg-[#0b1116]/80 px-2.5 py-1.5 backdrop-blur">
        <div v-for="r in regions" :key="r.id" class="flex items-center gap-2">
          <span class="h-1.5 w-1.5 rounded-full" :style="{ background: r.color }" />
          <span class="font-mono text-[9px] tracking-wider text-[#9aa7b2]">{{ r.id }}</span>
        </div>
      </div>
    </div>
  </section>
</template>