// Secure inputs — disable browser autocomplete for pasted secrets (API keys, tokens).
// Any <input> / <textarea> with class="secure" is hardened to autocomplete="off" and its
// parent <form> is also set to autocomplete="off". Works for static Svelte inputs via
// use:secure and for dynamically injected adapter HTML via hardenSecureElements + App observer.
//
// Password fields (type="password" with autocomplete="current-password" / "new-password")
// are intentionally NOT marked secure — password managers handle those separately.

function harden(el: HTMLInputElement | HTMLTextAreaElement): void {
  el.setAttribute('autocomplete', 'off')
  const form = el.closest('form')
  if (form && form.getAttribute('autocomplete') !== 'off') {
    form.setAttribute('autocomplete', 'off')
  }
}

export function secure(node: HTMLInputElement | HTMLTextAreaElement): { destroy(): void } {
  harden(node)
  return { destroy() {} }
}

export function hardenSecureElements(root: ParentNode = document): void {
  root.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input.secure, textarea.secure').forEach(harden)
}
