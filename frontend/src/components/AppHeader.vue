<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useDeviceState } from '../composables/useDeviceState'

const {
  uptimeLabel,
  connection,
  connectedPort,
  backendAvailable,
  availablePorts,
  connectingPort,
  connectToPort,
  disconnectPort,
} = useDeviceState()

const settingsOpen = ref(false)
const settingsRef = ref<HTMLElement | null>(null)

function toggleSettings() {
  settingsOpen.value = !settingsOpen.value
}

function closeSettings() {
  settingsOpen.value = false
}

function handleClickOutside(e: MouseEvent) {
  if (settingsOpen.value && settingsRef.value && !settingsRef.value.contains(e.target as Node)) {
    closeSettings()
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') closeSettings()
}

onMounted(() => {
  document.addEventListener('mousedown', handleClickOutside)
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('mousedown', handleClickOutside)
  document.removeEventListener('keydown', handleKeydown)
})

async function handlePortClick(port: string) {
  if (port === connectedPort.value) return
  await connectToPort(port)
}
</script>

<template>
  <header
    class="flex items-center justify-between px-5 py-3 rounded-2xl
           bg-[#0e141a] border border-[#1b2530]"
  >
    <!-- Brand + system status -->
    <div class="flex items-center gap-4">
      <h1 class="text-[#e6edf2] font-semibold tracking-wide text-sm sm:text-base">
        LAMP-THERM
        <span class="text-[#8a99a6] font-normal">v0.1.0</span>
      </h1>

      <span class="h-4 w-px bg-[#27333f]" />

      <div class="flex items-center gap-2 font-mono text-[11px] tracking-[0.2em] uppercase text-[#7c8b98]">
        <span
          class="h-1.5 w-1.5 rounded-full"
          :class="connection === 'connected' ? 'bg-[#2dd4bf] shadow-[0_0_8px_#2dd4bf]' : 'bg-[#f59e0b]'"
        />
        System Active: <span class="text-[#aab8c4]">{{ connectedPort || '—' }}</span>
      </div>
    </div>

    <!-- Uptime + controls -->
    <div class="flex items-center gap-3">
        
      <!-- Sensor settings: gear + COM port dropdown -->
      <div class="relative" ref="settingsRef">
        <button
          class="grid place-items-center h-8 w-8 rounded-lg bg-[#0b1116] border border-[#1b2530]
                 text-[#8a99a6] hover:text-[#2dd4bf] hover:border-[#234] transition-colors
                 focus:outline-none focus-visible:ring-2 focus-visible:ring-[#2dd4bf]/60"
          :class="settingsOpen ? 'text-[#2dd4bf] border-[#234]' : ''"
          aria-label="Sensor settings"
          aria-haspopup="listbox"
          :aria-expanded="settingsOpen"
          @click="toggleSettings"
        >
          <svg viewBox="0 0 24 24" class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M6 4v6M6 14v6M12 4v3M12 11v9M18 4v9M18 17v3" />
            <circle cx="6" cy="12" r="1.6" fill="currentColor" stroke="none" />
            <circle cx="12" cy="9" r="1.6" fill="currentColor" stroke="none" />
            <circle cx="18" cy="15" r="1.6" fill="currentColor" stroke="none" />
          </svg>
        </button>

        <div
          v-if="settingsOpen"
          role="listbox"
          class="absolute right-0 top-full mt-2 w-64 rounded-xl z-20
                 bg-[#0e141a] border border-[#1b2530] shadow-[0_12px_32px_rgba(0,0,0,0.45)]
                 p-2"
        >
          <div class="flex items-center justify-between px-2 pt-1 pb-2">
            <span class="text-[10px] tracking-[0.22em] uppercase text-[#5f6e7b] font-mono">
              COM Port
            </span>
            <button
              v-if="connectedPort"
              class="text-[10px] tracking-[0.1em] uppercase font-mono text-[#f59e0b]
                     hover:text-[#fbbf24] transition-colors"
              @click="disconnectPort"
            >
              Disconnect
            </button>
          </div>

          <!-- No Wails backend at all: nothing to list -->
          <div
            v-if="!backendAvailable"
            class="px-2 py-3 text-[11px] leading-snug text-[#5f6e7b] font-mono"
          >
            Demo mode — no device backend connected. Running on the synthetic sensor feed.
          </div>

          <!-- Backend present, still scanning -->
          <div
            v-else-if="availablePorts.length === 0"
            class="flex items-center gap-2 px-2 py-3 text-[11px] text-[#5f6e7b] font-mono"
          >
            <span class="h-1.5 w-1.5 rounded-full bg-[#f59e0b] animate-pulse" />
            Scanning for ports…
          </div>

          <!-- Port list -->
          <ul v-else class="flex flex-col gap-1 max-h-56 overflow-y-auto">
            <li v-for="port in availablePorts" :key="port">
              <button
                role="option"
                :aria-selected="port === connectedPort"
                class="w-full flex items-center justify-between gap-2 px-2.5 py-2 rounded-lg
                       text-left font-mono text-[12px] transition-colors
                       focus:outline-none focus-visible:ring-2 focus-visible:ring-[#2dd4bf]/60"
                :class="port === connectedPort
                  ? 'bg-[#0f1f1c] border border-[#1f3b39] text-[#7fe9d8] cursor-default'
                  : 'text-[#c2cdd6] hover:bg-[#121a22] border border-transparent'"
                :disabled="connectingPort !== null && connectingPort !== port"
                @click="handlePortClick(port)"
              >
                <span class="flex items-center gap-2 truncate">
                  <span
                    class="h-1.5 w-1.5 rounded-full shrink-0"
                    :class="port === connectedPort ? 'bg-[#2dd4bf] shadow-[0_0_6px_#2dd4bf]' : 'bg-[#3a4754]'"
                  />
                  <span class="truncate">{{ port }}</span>
                </span>

                <span
                  v-if="port === connectedPort"
                  class="text-[9px] tracking-[0.14em] uppercase text-[#2dd4bf] shrink-0"
                >
                  Connected
                </span>
                <span
                  v-else-if="connectingPort === port"
                  class="text-[9px] tracking-[0.14em] uppercase text-[#f59e0b] shrink-0"
                >
                  Connecting…
                </span>
              </button>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </header>
</template>