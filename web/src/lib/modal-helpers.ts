// Modal launch helpers. Single owner for the repeated resource.error reset
// + modal.open wizard stanza.
import { modal } from '../FUI/core/modal.svelte'

export function openFormModal(
  content: unknown,
  opts: {
    title: string
    size?: 'small' | 'medium' | 'large' | 'extra-large'
    props?: Record<string, unknown>
    onReload?: () => void
  }
): void {
  modal.open({
    title: opts.title,
    size: opts.size ?? 'medium',
    content: content as never,
    props: {
      ...(opts.props ?? {}),
      onComplete: () => {
        modal.close()
        opts.onReload?.()
      }
    }
  })
}
