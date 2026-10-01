<template>
  <router-view />
</template>

<style>
body {
  margin: 0;
  background: #f7f8fa;
  font-family: -apple-system, BlinkMacSystemFont, 'Helvetica Neue', Helvetica, Segoe UI, Arial, Roboto,
    'PingFang SC', 'miui', 'Hiragino Sans GB', 'Microsoft Yahei', sans-serif;
  -webkit-tap-highlight-color: transparent;
}

/* iOS 安全区：底部弹层内容避让 Home 指示条（viewport-fit=cover 后生效） */
.van-popup--bottom {
  padding-bottom: env(safe-area-inset-bottom);
}
#app {
  min-height: 100vh;
}
.page {
  padding: 12px 12px 60px;
}
</style>
