import './styles/tokens.css'
import './styles/base.css'
import './app.css'
import './lib/accent.svelte'
import { mount } from 'svelte'
import App from './App.svelte'
import { startAutoSquircle } from './lib/squircle'

startAutoSquircle()

const app = mount(App, { target: document.getElementById('app')! })

export default app
