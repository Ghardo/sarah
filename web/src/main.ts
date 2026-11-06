import { createApp } from 'vue'
import App from './App.vue'
import { initI18n } from './i18n'
import './style.css'

// Load the language before the first render so nothing flashes in English
initI18n().finally(() => createApp(App).mount('#app'))
