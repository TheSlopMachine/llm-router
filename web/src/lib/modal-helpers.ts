// Modal launch helpers. Single owner for the repeated resource.error reset
// + modal.open wizard stanza.
import { modal, type ModalSize } from '@fuckyouryan/fui'

export function openFormModal(
  content: unknown,
  options: {
    title: string
    size?: ModalSize
    props?: Record<string, unknown>
    onReload?: () => void
  }
): void {
  modal.open({
    title: options.title,
    size: options.size ?? 'md',
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
