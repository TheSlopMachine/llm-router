export type ModalSeverity = 'low' | 'medium' | 'high'
export type ModalSize = 'small' | 'medium' | 'large' | 'extra-large'

export interface ModalButton {
  label: string
  variant?: 'primary' | 'secondary' | 'danger'
  onClick: () => void | Promise<void>
  disabled?: boolean
  loading?: boolean
}

export interface ModalMenuAction {
  id: string
  label: string
  icon?: string
  disabled?: boolean
  danger?: boolean
}

export interface ModalMenu {
  label: string
  actions: ModalMenuAction[]
  onaction: (id: string) => void
}

export interface StepperConfig {
  current: number
  total: number
  labels: string[]
}

export interface BaseModalConfig {
  title: string
  subtitle?: string
  stepper?: StepperConfig | null
  footerHint?: string
  severity?: ModalSeverity
  size?: ModalSize
  buttons?: ModalButton[]
  menu?: ModalMenu | null
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

let stack = $state<ModalConfig[]>([])

export const modal = {
  get stack(): ModalConfig[] {
    return stack
  },

  confirm(config: Omit<ConfirmModalConfig, 'type'>): Promise<boolean> {
    return new Promise((resolve) => {
      const confirmText = config.confirmText || 'Confirm'
      const cancelText = config.cancelText || 'Cancel'

      stack = [
        ...stack,
        {
          type: 'confirm' as const,
          severity: config.severity || 'medium',
          size: config.size || 'small',
          ...config,
          buttons: [
            {
              label: cancelText,
              variant: 'secondary' as const,
              onClick: () => {
                modal.close()
                resolve(false)
              }
            },
            {
              label: confirmText,
              variant: (config.danger ? 'danger' : 'primary') as ModalButton['variant'],
              onClick: () => {
                modal.close()
                resolve(true)
              }
            }
          ]
        }
      ]
    })
  },

  open(config: Omit<ContentModalConfig, 'type'>): void {
    stack = [
      ...stack,
      {
        type: 'content' as const,
        severity: config.severity || 'medium',
        size: config.size || 'medium',
        buttons: config.buttons || [],
        ...config
      }
    ]
  },

  close(): void {
    const closedModal = stack[stack.length - 1]
    stack = stack.slice(0, -1)
    if (closedModal?.onClose) {
      closedModal.onClose()
    }
  },

  closeAll(): void {
    const toClose = [...stack]
    stack = []
    for (const m of toClose) {
      if (m.onClose) m.onClose()
    }
  },

  updateButtons(buttons: ModalButton[]): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    topModal.buttons = buttons
    newStack[newStack.length - 1] = topModal
    stack = newStack
  },

  updateMenu(menu: ModalMenu | null): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    topModal.menu = menu
    newStack[newStack.length - 1] = topModal
    stack = newStack
  },

  updateTitle(title: string): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    topModal.title = title
    newStack[newStack.length - 1] = topModal
    stack = newStack
  },

  updateSubtitle(subtitle: string): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    topModal.subtitle = subtitle
    newStack[newStack.length - 1] = topModal
    stack = newStack
  },

  updateStepper(stepper: StepperConfig | null): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    topModal.stepper = stepper
    newStack[newStack.length - 1] = topModal
    stack = newStack
  },

  updateFooterHint(hint: string): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    topModal.footerHint = hint
    newStack[newStack.length - 1] = topModal
    stack = newStack
  },

  updateProps(props: Record<string, any>): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = { ...newStack[newStack.length - 1] } as ModalConfig
    if (topModal.type === 'content') {
      topModal.props = { ...topModal.props, ...props }
    }
    newStack[newStack.length - 1] = topModal
    stack = newStack
  }
}
