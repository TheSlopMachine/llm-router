import { t } from './i18n.svelte'

export type ModalSeverity = 'low' | 'medium' | 'high'
export type ModalSize = 'small' | 'medium' | 'large' | 'extra-large'
export type ConfirmRole = 'default' | 'destructive'

export interface ModalButton {
  label: string
  variant?: 'primary' | 'secondary' | 'danger' | 'text' | 'text danger' | 'text plain'
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
  severity?: ModalSeverity
  size?: ModalSize
  className?: string
  buttons?: ModalButton[]
  menu?: ModalMenu | null
  onClose?: () => void
}

export interface ConfirmModalConfig extends BaseModalConfig {
  type: 'confirm'
  message?: string
  content?: any
  props?: Record<string, any>
  confirmText?: string
  confirmRole?: ConfirmRole
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
      stack = [
        ...stack,
        {
          type: 'confirm' as const,
          severity: config.severity || 'medium',
          size: config.size || 'small',
          ...config,
          onClose: () => {
            resolve(false)
            config.onClose?.()
          },
          buttons: [
            {
              label: t('Cancel'),
              variant: 'text plain' as ModalButton['variant'],
              onClick: () => {
                modal.close()
              }
            },
            {
              label: config.confirmText || t('Confirm'),
              variant: (config.confirmRole === 'destructive' ? 'danger' : 'primary') as ModalButton['variant'],
              onClick: () => {
                resolve(true)
                modal.close()
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
