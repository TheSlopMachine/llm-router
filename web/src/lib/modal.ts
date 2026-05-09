import { writable } from 'svelte/store'

export type ModalSeverity = 'low' | 'medium' | 'high'
export type ModalSize = 'small' | 'medium' | 'large' | 'extra-large'

export interface ModalButton {
  label: string
  variant?: 'primary' | 'secondary' | 'danger'
  onClick: () => void | Promise<void>
  disabled?: boolean
  loading?: boolean
}

export interface BaseModalConfig {
  title: string
  severity?: ModalSeverity
  size?: ModalSize
  buttons?: ModalButton[]
  onClose?: () => void
}

export interface ConfirmModalConfig extends BaseModalConfig {
  type: 'confirm'
  message: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

export interface ContentModalConfig extends BaseModalConfig {
  type: 'content'
  content: any
  props?: Record<string, any>
}

export type ModalConfig = ConfirmModalConfig | ContentModalConfig

interface ModalState {
  stack: ModalConfig[]
}

const { subscribe, update } = writable<ModalState>({ stack: [] })

export const modal = {
  subscribe,
  
  confirm(config: Omit<ConfirmModalConfig, 'type'>): Promise<boolean> {
    return new Promise((resolve) => {
      const confirmText = config.confirmText || 'Confirm'
      const cancelText = config.cancelText || 'Cancel'
      
      update(state => ({
        stack: [...state.stack, {
          type: 'confirm' as const,
          severity: config.severity || 'medium',
          size: config.size || 'small',
          ...config,
          buttons: [
            { 
              label: cancelText, 
              variant: 'secondary', 
              onClick: () => {
                modal.close()
                resolve(false)
              }
            },
            { 
              label: confirmText, 
              variant: config.danger ? 'danger' : 'primary', 
              onClick: () => {
                modal.close()
                resolve(true)
              }
            }
          ]
        }]
      }))
    })
  },
  
  open(config: Omit<ContentModalConfig, 'type'>) {
    update(state => ({
      stack: [...state.stack, {
        type: 'content' as const,
        severity: config.severity || 'medium',
        size: config.size || 'medium',
        buttons: config.buttons || [],
        ...config
      }]
    }))
  },
  
  close() {
    update(state => {
      const newStack = state.stack.slice(0, -1)
      const closedModal = state.stack[state.stack.length - 1]
      if (closedModal?.onClose) {
        closedModal.onClose()
      }
      return { stack: newStack }
    })
  },
  
  closeAll() {
    update(state => {
      state.stack.forEach(modal => {
        if (modal.onClose) {
          modal.onClose()
        }
      })
      return { stack: [] }
    })
  },
  
  updateButtons(buttons: ModalButton[]) {
    update(state => {
      if (state.stack.length === 0) return state
      const newStack = [...state.stack]
      const topModal = { ...newStack[newStack.length - 1] }
      topModal.buttons = buttons
      newStack[newStack.length - 1] = topModal
      return { stack: newStack }
    })
  },
  
  updateTitle(title: string) {
    update(state => {
      if (state.stack.length === 0) return state
      const newStack = [...state.stack]
      const topModal = { ...newStack[newStack.length - 1] }
      topModal.title = title
      newStack[newStack.length - 1] = topModal
      return { stack: newStack }
    })
  },

  updateProps(props: Record<string, any>) {
    update(state => {
      if (state.stack.length === 0) return state
      const newStack = [...state.stack]
      const topModal = { ...newStack[newStack.length - 1] }
      if (topModal.type === 'content') {
        topModal.props = { ...topModal.props, ...props }
      }
      newStack[newStack.length - 1] = topModal
      return { stack: newStack }
    })
  }
}
