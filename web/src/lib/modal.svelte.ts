import { t } from './i18n.svelte'
import type { Snippet } from 'svelte'

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
  tint?: string
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

// Inline dialog: the body is a caller snippet closing over local state,
// no component file needed for trivial prompts.
export interface SnippetModalConfig extends BaseModalConfig {
  type: 'snippet'
  contentSnippet: Snippet
}

export type ModalConfig = ConfirmModalConfig | ContentModalConfig | SnippetModalConfig

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
          // Title is mandatory for confirm modals: fall back rather than
          // render a headless dialog.
          title: config.title?.trim() ? config.title : t('Confirm'),
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

  open(config: Omit<ContentModalConfig, 'type'> | Omit<SnippetModalConfig, 'type'>): void {
    const type = ('contentSnippet' in config ? 'snippet' : 'content') as ModalConfig['type']
    stack = [
      ...stack,
      {
        type,
        severity: config.severity || 'medium',
        size: config.size || 'medium',
        buttons: config.buttons || [],
        ...config
      } as ModalConfig
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

  // Chrome updates mutate the top config in place. The Modal shell keys
  // its each-block by config identity, so replacing the object would
  // destroy and recreate the whole content subtree on every update —
  // wiping content state, focus, and in-flight clicks.
  updateButtons(buttons: ModalButton[]): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = newStack[newStack.length - 1] as ModalConfig
    topModal.buttons = buttons
    stack = newStack
  },

  updateMenu(menu: ModalMenu | null): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = newStack[newStack.length - 1] as ModalConfig
    topModal.menu = menu
    stack = newStack
  },

  updateTitle(title: string): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = newStack[newStack.length - 1] as ModalConfig
    topModal.title = title
    stack = newStack
  },

  updateSubtitle(subtitle: string): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = newStack[newStack.length - 1] as ModalConfig
    topModal.subtitle = subtitle
    stack = newStack
  },

  updateStepper(stepper: StepperConfig | null): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = newStack[newStack.length - 1] as ModalConfig
    topModal.stepper = stepper
    stack = newStack
  },

  updateProps(props: Record<string, any>): void {
    if (stack.length === 0) return
    const newStack = [...stack]
    const topModal = newStack[newStack.length - 1] as ModalConfig
    if (topModal.type === 'content') {
      topModal.props = { ...topModal.props, ...props }
    }
    stack = newStack
  }
}
