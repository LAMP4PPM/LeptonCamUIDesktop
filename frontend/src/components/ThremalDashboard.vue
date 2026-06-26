<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useDeviceState } from '../composables/useDeviceState'
import AppHeader from './AppHeader.vue'
import ThermalStream from './ThermalStream.vue'
import AreaSelection from './AreaSelection.vue'
import RecordingPanel from './Recording.vue'
import FluctuationsChart from './Fluctuation.vue'

const { connect, disconnect } = useDeviceState()

onMounted(connect)
onUnmounted(disconnect)
</script>

<template>
  <div class="min-h-screen bg-[#080b0f] p-4 text-[#e6edf2] selection:bg-[#2dd4bf]/30">
    <div class="mx-auto flex max-w-[1400px] flex-col gap-4">
      <AppHeader />

      <div class="grid grid-cols-1 gap-4 lg:grid-cols-[2fr_1fr]">
        <ThermalStream />
        <div class="flex flex-col gap-4">
          <AreaSelection />
          <RecordingPanel />
        </div>
      </div>

      <FluctuationsChart />
    </div>
  </div>
</template>