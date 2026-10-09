// Widget library barrel.
//
// Single import point so call sites read `from '$ui'`-style instead of
// counting '../../' levels, and so moving a file inside the library does
// not touch every page that uses it.
//
// Layout primitives -- structural, no scoped styles, no domain knowledge.
export { default as Stack } from './layout/Stack.svelte'
export { default as VStack } from './layout/VStack.svelte'
export { default as HStack } from './layout/HStack.svelte'
export { default as ZStack } from './layout/ZStack.svelte'
export { default as Grid } from './layout/Grid.svelte'
export { default as Wrap } from './layout/Wrap.svelte'
export { default as Spacer } from './layout/Spacer.svelte'
export { default as ScrollView } from './layout/ScrollView.svelte'
export { default as Box } from './layout/Box.svelte'
export type { Step, Align, Justify, Size, FillSize } from './tokens'
export { fillStyle } from './tokens'

// Controls.
export { default as Divider } from './controls/Divider.svelte'
export { default as Icon } from './controls/Icon.svelte'
export { default as Text } from './controls/Text.svelte'
export { default as Image } from './controls/Image.svelte'
export { default as Button } from './controls/Button.svelte'
export { default as Switch } from './controls/Switch.svelte'
export { default as Checkbox } from './controls/Checkbox.svelte'
export { default as Chip } from './controls/Chip.svelte'
export { default as Select } from './controls/Select.svelte'
export { default as FloatingList } from './controls/FloatingList.svelte'
export type { FloatingAction } from './controls/FloatingList.svelte'
export { default as FloatingView } from './controls/FloatingView.svelte'
export { default as SearchField } from './controls/SearchField.svelte'
export { default as TextEdit } from './controls/TextEdit.svelte'
export type { TrailingAction } from './controls/TextEdit.svelte'
export { default as TextArea } from './controls/TextArea.svelte'
// SwiftUI calls this Picker; it was named SegmentedControl before.
export { default as Picker } from './controls/Picker.svelte'

// Composites.
export { default as Header } from './composite/Header.svelte'
export { default as Stat } from './composite/Stat.svelte'
export { default as Modal } from './composite/Modal.svelte'
export { default as StepsView } from './composite/StepsView.svelte'
export { default as CopyButton } from './composite/CopyButton.svelte'
export { default as List } from './composite/List.svelte'
export { default as SectionCard } from './composite/SectionCard.svelte'
export { default as Banner } from './composite/Banner.svelte'
export { default as CodeBlock } from './composite/CodeBlock.svelte'
export { default as Table } from './composite/Table.svelte'
export type { TableColumn, TableSortDir } from './composite/Table.svelte'
export { default as ConfirmAction } from './composite/ConfirmAction.svelte'
export { default as FilePicker } from './composite/FilePicker.svelte'
export { default as Toolbar } from './composite/Toolbar.svelte'
export { default as ToolbarItem } from './composite/ToolbarItem.svelte'

// Components added when the library was split from the site
export { default as Toasts } from './composite/Toasts.svelte'
export { default as EmptyState } from './composite/EmptyState.svelte'
export { default as Link } from './controls/Link.svelte'

// Library state / helpers
export * from './core/texts'
export * from './core/theme.svelte'
export * from './core/accent.svelte'
export * from './core/modal.svelte'
export * from './core/toast.svelte'
export * from './core/squircle'
export * from './core/squircle-baked'
