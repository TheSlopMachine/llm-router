import type { IconName, Tint } from '@fuckyouryan/fui'

// Modality presentation: icon + chip tint per modality name. Shared by
// ModelsTable and every custom view rendering modality chips.

export function modalityTint(mod: string): Tint {
  switch (mod) {
    case 'text': return 'success'
    case 'image': return 'purple'
    case 'audio':
    case 'speech':
    case 'transcription': return 'teal'
    case 'video': return 'orange'
    case 'file': return 'warning'
    case 'pdf': return 'danger'
    case 'embedding': return 'neutral'
    default: return 'neutral'
  }
}

export function modalityIcon(mod: string): IconName {
  switch (mod) {
    case 'text': return 'title'
    case 'image': return 'image'
    case 'audio': return 'graphic_eq'
    case 'file': return 'attach_file'
    case 'pdf': return 'picture_as_pdf'
    case 'video': return 'videocam'
    case 'embedding': return 'scatter_plot'
    case 'speech': return 'record_voice_over'
    case 'transcription': return 'hearing'
    default: return 'help_outline'
  }
}
