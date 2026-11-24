// Site-side wiring for the texts and the locale the widget library must not own.
import { setFuiLocale, setFuiTexts } from '@fuckyouryan/fui'
import { locale, t } from './i18n.svelte'

setFuiLocale(locale)

setFuiTexts(() => ({
  close: t('common.actions.close'),
  confirm: t('common.actions.confirm'),
  cancel: t('common.actions.cancel'),
  delete: t('common.actions.delete'),
  irreversible: t('fui.irreversible'),
  clear: t('fui.clear'),
  showSecret: t('fui.show_secret'),
  hideSecret: t('fui.hide_secret'),
  select: t('fui.select'),
  more: t('fui.more'),
  menu: t('nav.menu.title'),
  chooseFile: t('fui.choose_file'),
  searchPlaceholder: t('common.search.placeholder'),
  noOptions: t('common.empty.no_options'),
  copy: t('common.actions.copy'),
  copyFailed: t('fui.copy_failed'),
  loading: t('common.state.loading'),
  expand: t('fui.expand'),
  collapse: t('fui.collapse'),
}))
