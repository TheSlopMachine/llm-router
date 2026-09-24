// Modality presentation: icon + chip color per modality name. Shared by
// ModelsTable and every custom view rendering modality chips.

export function modalityColor(mod: string): string {
  switch (mod) {
    case 'text': return 'chip-green'
    case 'image': return 'chip-purple'
    case 'audio':
    case 'speech':
    case 'transcription': return 'chip-teal'
    case 'video': return 'chip-orange'
    case 'file': return 'chip-yellow'
    case 'embedding': return 'chip-neutral'
    default: return 'chip-neutral'
  }
}

export function modalityIcon(mod: string): string {
  switch (mod) {
    case 'text': return 'title'
    case 'image': return 'image'
    case 'audio': return 'graphic_eq'
    case 'file': return 'attach_file'
    case 'video': return 'videocam'
    case 'embedding': return 'scatter_plot'
    case 'speech': return 'record_voice_over'
    case 'transcription': return 'hearing'
    default: return 'help_outline'
  }
}
