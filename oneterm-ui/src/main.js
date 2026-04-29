import Vue from 'vue'
import App from './App.vue'
import router from './router'
import store from './store/'
import bootstrap from './core/bootstrap'
import './core/use'
import './guard' // guard permission control
import './utils/filter' // global filter
import Setting from './config/setting'
import { Icon } from 'ant-design-vue'
import i18n from './lang'

// iconfont.cn-generated script that sets window._iconfont_svg_string_*.
// Loaded as a <script> tag from /iconfont/iconfont.js in index.html so the
// 3.9 MB blob ships as its own asset instead of inflating the main JS
// bundle. The original code's default-import binding was always undefined
// regardless; only the global window mutations matter.
const iconFont = undefined

const customIcon = Icon.createFromIconfontCN(iconFont)
Vue.component('ops-icon', customIcon)
var vue

async function start() {
  const _vue = new Vue({
    router,
    store,
    i18n,
    created: bootstrap,
    render: h => h(App)
  }).$mount('#app')
  vue = _vue

  if (process.env.NODE_ENV === 'development') {
    window.$app = vue
    window.$router = router
    window.$store = store
    window.$env = process.env
  }
}

start()
window.$setting = Setting

export default vue
