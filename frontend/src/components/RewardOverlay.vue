<template>
  <!-- 完成任务的奖励结算动效：两段式（任务积分 → 宝箱开出） -->
  <van-overlay :show="visible" z-index="3000" class="reward-mask" @click.stop>
    <div class="reward-card" v-if="visible">
      <template v-if="phase === 'task'">
        <div class="reward-label">{{ data.taskTitle }}</div>
        <div class="reward-amount bounce">+{{ data.taskPoints }}</div>
        <div class="reward-unit">积分到账</div>
      </template>
      <template v-else-if="phase === 'box'">
        <div class="box-icon pop">🎁</div>
        <div class="reward-label">{{ data.boxName }}</div>
        <div class="reward-amount bounce gold">+{{ data.boxPoints }}</div>
        <div class="reward-unit">宝箱开出！</div>
      </template>
      <template v-else>
        <div class="reward-total">共 +{{ data.total }} 积分</div>
      </template>
      <van-button round type="primary" class="reward-btn" @click="close">收下</van-button>
    </div>
  </van-overlay>
</template>

<script setup>
import { ref, watch } from 'vue'
import { playDing } from '../sound'

const props = defineProps({ result: Object })
const emit = defineEmits(['close'])

const visible = ref(false)
const phase = ref('task')
const data = ref({})

watch(
  () => props.result,
  (r) => {
    if (!r) return
    data.value = {
      taskTitle: r.task && r.task.title,
      taskPoints: r.task && r.task.points,
      boxName: r.box && r.box.name,
      boxPoints: r.box && r.box.points,
      total: r.earned
    }
    visible.value = true
    phase.value = 'task'
    playDing()
    if (r.box) {
      setTimeout(() => {
        phase.value = 'box'
        playDing()
      }, 1200)
    }
  }
)

function close() {
  visible.value = false
  emit('close')
}
</script>

<style scoped>
.reward-mask {
  display: flex;
  align-items: center;
  justify-content: center;
}
.reward-card {
  width: 78%;
  max-width: 340px;
  background: #fff;
  border-radius: 20px;
  padding: 36px 20px 24px;
  text-align: center;
}
.reward-label {
  color: #969799;
  font-size: 14px;
  margin-bottom: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.reward-amount {
  font-size: 56px;
  font-weight: 700;
  color: #1989fa;
  line-height: 1.1;
}
.reward-amount.gold {
  color: #ff9900;
}
.reward-unit {
  margin-top: 6px;
  color: #969799;
  font-size: 13px;
}
.reward-total {
  font-size: 20px;
  font-weight: 600;
  color: #323233;
  margin: 12px 0 4px;
}
.box-icon {
  font-size: 64px;
  line-height: 1.2;
}
.reward-btn {
  margin-top: 24px;
  width: 60%;
}
.bounce {
  animation: bounce 0.5s cubic-bezier(0.18, 0.89, 0.32, 1.28);
}
.pop {
  animation: pop 0.45s cubic-bezier(0.18, 0.89, 0.32, 1.28);
}
@keyframes bounce {
  0% { transform: scale(0.3); opacity: 0; }
  100% { transform: scale(1); opacity: 1; }
}
@keyframes pop {
  0% { transform: scale(0.2) rotate(-12deg); opacity: 0; }
  60% { transform: scale(1.15) rotate(4deg); }
  100% { transform: scale(1) rotate(0); opacity: 1; }
}
</style>
