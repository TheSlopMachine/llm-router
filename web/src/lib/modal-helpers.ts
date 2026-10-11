// Modal launch helpers. Single owner for the repeated resource.error reset
// + modal.open wizard stanza.
import { modal } from '../FUI/core/modal.svelte'

export function openFormModal(
  content: unknown,
  options: {
    title: string
    size?: 'small' | 'medium' | 'large' | 'extra-large'
    props?: Record<string, unknown>
    onReload?: () => void
  }
): void {
  modal.open({
    title: options.title,
    size: options.size ?? 'medium',
    content: content as never,
    props: {
      ...(options.props ?? {}),
      onComplete: () => {
        modal.close()
        options.onReload?.()
      }
    }
  })
}
