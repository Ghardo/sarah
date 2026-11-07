import { createApp } from 'vue'
import App from './App.vue'
import { loadDefaults } from './defaults'
import { initI18n } from './i18n'
import './style.css'

// Load the language and the defaults before the first render, so nothing
// briefly shows in English or with wrong defaults
Promise.allSettled([initI18n(), loadDefaults()]).finally(() => createApp(App).mount('#app'))
