<template>
  <div>
    <router-view />
    <van-tabbar route safe-area-inset-bottom>
      <van-tabbar-item to="/tasks" icon="todo-list-o">任务</van-tabbar-item>
      <van-tabbar-item to="/stats" icon="bar-chart-o">统计</van-tabbar-item>
      <van-tabbar-item to="/shop" icon="gift-o">商城</van-tabbar-item>
      <van-tabbar-item to="/mine" icon="user-o">我的</van-tabbar-item>
    </van-tabbar>
  </div>
</template>
