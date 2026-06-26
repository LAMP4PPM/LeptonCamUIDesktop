<script setup lang="ts">
import { computed } from 'vue'
import { useDeviceState } from '../composables/useDeviceState'

const {
    recording,
    recordingLabel,
    storageLabel,
    captureRate,
    toggleRecording,
    setCaptureRate,
} = useDeviceState()

const rateOptions = [1, 2, 5, 10, 20, 50]

const rateInput = computed({
    get: () => String(captureRate.value),
    set: (v: string) => {
        const n = parseInt(v, 10)
        if (!Number.isNaN(n)) setCaptureRate(n)
    },
})
</script>

<template>
    <section class="rounded-2xl border border-[#1b2530] bg-[#0e141a] p-4">
        <h2 class="flex items-center gap-2 font-mono text-[11px] tracking-[0.2em] uppercase text-[#8a99a6]">
            <svg viewBox="0 0 24 24" class="h-4 w-4 text-[#5f6e7b]" fill="none" stroke="currentColor" stroke-width="2"
                stroke-linecap="round">
                <circle cx="12" cy="13" r="8" />
                <path d="M12 9v4l2.5 2M9 2h6" />
            </svg>
            Recording Duration
        </h2>

        <!-- Timer -->
        <div class="mt-3 text-center">
            <span class="font-mono text-4xl font-semibold tabular-nums tracking-wider"
                :class="recording ? 'text-[#e6edf2]' : 'text-[#5f6e7b]'">
                {{ recordingLabel }}
            </span>
        </div>

        <!-- Status line -->
        <div
            class="mt-2 flex items-center justify-center gap-2 font-mono text-[10px] tracking-[0.12em] uppercase text-[#7c8b98]">
            <span class="h-1.5 w-1.5 rounded-full"
                :class="recording ? 'bg-[#f59e0b] shadow-[0_0_8px_#f59e0b] animate-pulse' : 'bg-[#3a4651]'" />
            <span :class="recording ? 'text-[#c2cdd6]' : ''">
                {{ recording ? 'Rec Active' : 'Standby' }} • Storage
                {{ storageLabel }}
            </span>
        </div>

        <!-- Capture rate -->
        <div class="mt-4 flex items-center justify-between font-mono text-[10px] tracking-[0.18em] uppercase">
            <span class="text-[#7c8b98]">Capture Rate</span>
        </div>

        <div class="mt-2 grid grid-cols-1">
            <div class="flex items-center rounded-lg border border-[#27333f] bg-[#0b1116] px-3">
                <select v-model.number="rateInput"
                    class="w-full bg-transparent py-2 font-mono text-sm text-[#e6edf2] tabular-nums outline-none appearance-none cursor-pointer">
                    <option v-for="n in rateOptions" :key="n" :value="n" class="bg-[#0d1117] text-[#e6edf2]">
                        {{ n }}
                    </option>
                </select>
                <span class="font-mono text-[10px] tracking-widest text-[#5f6e7b]">SEC</span>
            </div>
        </div>

        <!-- Record button -->
        <button class="mt-4 flex w-full items-center justify-center gap-3 rounded-xl border py-3.5
             font-mono text-xs tracking-[0.2em] uppercase transition-all" :class="recording
                ? 'border-[#f59e0b]/70 bg-[#f59e0b]/10 text-[#f5b056] shadow-[0_0_24px_-6px_#f59e0b]'
                : 'border-[#2dd4bf]/70 bg-[#2dd4bf]/10 text-[#2dd4bf] shadow-[0_0_24px_-6px_#2dd4bf] hover:bg-[#2dd4bf]/15'"
            @click="toggleRecording()">
            <span class="grid place-items-center h-4 w-4 rounded-full border"
                :class="recording ? 'border-[#f5b056]' : 'border-[#2dd4bf]'">
                <span class="h-2 w-2 rounded-full" :class="recording ? 'bg-[#f5b056] animate-pulse' : 'bg-[#2dd4bf]'" />
            </span>
            {{ recording ? 'Stop Recording' : 'Record Stream + Data' }}
        </button>
    </section>
</template>