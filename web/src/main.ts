import { createApp } from 'vue'
import App from './App.vue'
import { initI18n } from './i18n'
import './style.css'

// Sprache vor dem ersten Rendern laden, damit nichts kurz auf Englisch aufblitzt
initI18n().finally(() => createApp(App).mount('#app'))
