import './FUI/styles/tokens.css'
import './FUI/styles/base.css'
import './styles/reset.css'
import './app.css'
import './lib/accent.svelte'
import './lib/fui-setup'
import { mount } from 'svelte'
import App from './App.svelte'

const app = mount(App, { target: document.getElementById('app')! })

export default app
