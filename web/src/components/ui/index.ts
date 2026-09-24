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
export { default as Spacer } from './layout/Spacer.svelte'
export { default as ScrollView } from './layout/ScrollView.svelte'
export { default as Box } from './layout/Box.svelte'
export type { Step, Align, Justify, Size } from './tokens'

// Controls.
export { default as Divider } from './controls/Divider.svelte'
export { default as Text } from './controls/Text.svelte'
export { default as Image } from './controls/Image.svelte'
export { default as Button } from './controls/Button.svelte'
export { default as Switch } from './controls/Switch.svelte'
export { default as Checkbox } from './controls/Checkbox.svelte'
export { default as Chip } from './controls/Chip.svelte'
export { default as Select } from './controls/Select.svelte'
export { default as FloatingList } from './controls/FloatingList.svelte'
export type { FloatingAction } from './controls/FloatingList.svelte'
export { default as SearchField } from './controls/SearchField.svelte'
export { default as TextEdit } from './controls/TextEdit.svelte'
export type { TrailingAction } from './controls/TextEdit.svelte'
export { default as TextArea } from './controls/TextArea.svelte'
// SwiftUI calls this Picker; it was named SegmentedControl before.
export { default as Picker } from './controls/Picker.svelte'

// Composites.
export { default as Modal } from './composite/Modal.svelte'
export { default as StepsView } from './composite/StepsView.svelte'
export { default as CopyButton } from './composite/CopyButton.svelte'
export { default as List } from './composite/List.svelte'
export { default as SectionCard } from './composite/SectionCard.svelte'
export { default as CodeBlock } from './composite/CodeBlock.svelte'
export { default as Table } from './composite/Table.svelte'
export type { TableColumn, TableSortDir } from './composite/Table.svelte'
export { default as ModelsTable } from './composite/ModelsTable.svelte'
export { default as ModalitiesFlow } from './composite/ModalitiesFlow.svelte'
export type { ModelsTableModel, ModelsTableActions } from './composite/ModelsTable.svelte'
