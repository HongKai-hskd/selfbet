<template>
  <div class="page">
    <van-nav-bar title="道具图鉴" left-arrow @click-left="$router.back()" />

    <div class="tip">道具通过开启宝箱有概率获得（概率由各宝箱的「道具概率」配置决定），在背包的道具区点按使用。</div>

    <div v-for="it in items" :key="it.id" class="guide-card">
      <div class="g-icon" :style="{ background: it.bg }">{{ it.icon }}</div>
      <div class="g-main">
        <div class="g-name">{{ it.name }}</div>
        <div class="g-desc">{{ it.desc }}</div>
        <div class="g-use">使用：背包 → 道具区 → 点按该道具</div>
      </div>
    </div>

    <div class="rule-card">
      <div class="rule-title">通用规则</div>
      <div class="rule-row">· 道具使用即消耗（一张一次），使用会写入流水记录</div>
      <div class="rule-row">· 道具产出的积分/余额入账后不可撤回</div>
      <div class="rule-row">· 数量在背包中堆叠显示（×N）</div>
      <div class="rule-row">· 触发型道具（免罚金牌/双倍卡/幸运符/运势卡）使用后进入待生效状态，触发即消耗并写入流水</div>
    </div>
  </div>
</template>

<script setup>
// 图鉴数据来自 /api/items/guide（后端 itemdef.go 注册表驱动），
// 道具池变更时本页自动跟进，无需改代码
import { ref, onMounted } from 'vue'
import api from '../api'

const items = ref([])
const BGS = ['#fff7e6', '#e8f7ee', '#eef3ff', '#f3eefe']
onMounted(async () => {
  const res = await api.get('/items/guide')
  items.value = (res.items || []).map((d, i) => ({ ...d, bg: BGS[i % BGS.length] }))
})
</script>

<style scoped>
.page { min-height: 100vh; background: #f7f8fa; padding-bottom: 30px; }
.tip {
  font-size: 12px;
  color: #969799;
  line-height: 1.6;
  background: #fffbeb;
  margin: 12px 16px 0;
  padding: 8px 12px;
  border-radius: 8px;
}
.guide-card {
  display: flex;
  gap: 12px;
  background: #fff;
  border-radius: 12px;
  padding: 14px;
  margin: 12px 16px 0;
  align-items: flex-start;
}
.g-icon {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  flex-shrink: 0;
}
.g-main { flex: 1; min-width: 0; }
.g-name { font-size: 15px; font-weight: 600; color: #323233; }
.g-desc { font-size: 13px; color: #646566; line-height: 1.6; margin-top: 4px; }
.g-use { font-size: 11px; color: #c8c9cc; margin-top: 4px; }
.rule-card {
  background: #fff;
  border-radius: 12px;
  padding: 12px 14px;
  margin: 14px 16px 0;
}
.rule-title { font-size: 13px; font-weight: 600; color: #323233; margin-bottom: 6px; }
.rule-row { font-size: 12px; color: #969799; line-height: 1.8; }
</style>
