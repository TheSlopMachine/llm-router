// Docker view helpers: what is busy, how far it got, what to tell the
// operator. Components render; these decide.
import { t, tx } from './i18n.svelte'
import type { TranslationKey } from './i18n.svelte'
import { formatBytes } from './format'
import type { DockerContainerStatus, DockerImageStatus, DockerPluginView, DockerStage } from './types'

/** a container on its way to running: created, restarting, or running with its health check pending */
export function containerStarting(container: DockerContainerStatus): boolean {
  return container.state === 'created' || container.state === 'restarting' || (container.state === 'running' && container.health === 'starting')
}

export function dockerBusy(view: DockerPluginView | null | undefined): boolean {
  if (!view) return false
  return view.images.some((image) => image.state === 'preparing') || view.containers.some(containerStarting)
}

/** what a provider page needs from its Docker section: whether a container is on its way, and one line on how far */
export interface DockerSectionState {
  busy: boolean
  waitText: string
}

export function imageName(image: DockerImageStatus): string {
  return image.build ?? image.reference
}

/** one line on where an image stands */
export function describeImage(image: DockerImageStatus): string {
  if (image.state === 'ready') return t('providers.docker.image_ready.status')
  if (image.state === 'failed') return t('providers.docker.failed.status')
  if (image.state === 'absent') return t('providers.docker.image_absent.status')
  const progress = image.progress
  if (!progress) return t('providers.docker.image_preparing.status')
  const bytes = progress.bytes_total ? tx('providers.docker.bytes.label', { done: formatBytes(progress.bytes_done ?? 0), total: formatBytes(progress.bytes_total) }) : ''
  switch (progress.phase) {
    case 'download_context':
      return bytes ? `${t('providers.docker.phase_download.status')}: ${bytes}` : t('providers.docker.phase_download.status')
    case 'pull':
      return bytes ? `${t('providers.docker.phase_pull.status')}: ${bytes}` : t('providers.docker.phase_pull.status')
    case 'build':
      if (progress.step && progress.steps) {
        return tx('providers.docker.build_step.status', { step: progress.step, steps: progress.steps, detail: progress.detail ?? '' })
      }
      return t('providers.docker.phase_build.status')
  }
}

/** a row of the Docker table: a container, or an image that is not ready yet */
export type DockerRow =
  | { kind: 'container'; id: string; name: string; container: DockerContainerStatus }
  | { kind: 'image'; id: string; name: string; image: DockerImageStatus }

export function containerTarget(container: DockerContainerStatus): string {
  return container.service ? `${container.key}/${container.service}` : container.key
}

/** containers first; a ready image needs no row, its containers show it */
export function dockerRows(view: DockerPluginView): DockerRow[] {
  const containers: DockerRow[] = view.containers.map((container) => ({ kind: 'container', id: `container:${containerTarget(container)}`, name: containerTarget(container), container }))
  const images: DockerRow[] = view.images
    .filter((image) => image.state === 'preparing' || image.state === 'failed')
    .map((image) => ({ kind: 'image', id: `image:${image.reference}`, name: imageName(image), image }))
  return [...containers, ...images]
}

export interface DockerStatusText {
  label: string
  tint?: 'success' | 'warning' | 'danger'
  caption: string
}

/** one status line and an optional caption for a container */
export function containerStatus(container: DockerContainerStatus): DockerStatusText {
  switch (container.state) {
    case 'running':
      if (container.health === 'unhealthy') return { label: t('providers.docker.unhealthy.status'), tint: 'danger', caption: '' }
      if (container.health === 'starting') return { label: t('providers.docker.starting.status'), tint: 'warning', caption: t('providers.docker.health_pending.status') }
      return { label: t('providers.docker.running.status'), tint: 'success', caption: container.health === 'healthy' ? t('providers.docker.healthy.status') : '' }
    case 'created':
    case 'restarting':
      return { label: t('providers.docker.starting.status'), tint: 'warning', caption: '' }
    case 'exited':
      return { label: t('providers.docker.stopped.status'), caption: container.exit_code ? tx('providers.docker.exit_code.status', { code: container.exit_code }) : '' }
    case 'dead':
      return { label: t('providers.docker.failed.status'), tint: 'danger', caption: '' }
    default:
      return { label: container.state, caption: '' }
  }
}

/** one line for pages waiting on the containers, or '' when nothing is busy */
export function describeWait(view: DockerPluginView | null | undefined): string {
  if (!view) return ''
  const preparing = view.images.find((image) => image.state === 'preparing')
  if (preparing) return describeImage(preparing)
  const starting = view.containers.find(containerStarting)
  if (starting) return starting.state === 'running' ? t('providers.docker.health_pending.status') : t('providers.docker.container_starting.status')
  return ''
}

/** what to fix for a failed connection stage */
export function stageHint(stage: DockerStage | undefined): string {
  const keys: Record<DockerStage, TranslationKey> = {
    connect: 'settings.docker.stage_connect.hint',
    handshake: 'settings.docker.stage_handshake.hint',
    host_key: 'settings.docker.stage_host_key.hint',
    authenticate: 'settings.docker.stage_authenticate.hint',
    socket: 'settings.docker.stage_socket.hint',
    local_socket: 'settings.docker.stage_local_socket.hint',
    tls: 'settings.docker.stage_tls.hint',
    daemon: 'settings.docker.stage_daemon.hint',
  }
  return stage ? t(keys[stage]) : ''
}

export type DockerEndpointKind = 'ssh' | 'unix' | 'tcp' | 'wsl'

export interface DockerEndpointParts {
  kind: DockerEndpointKind
  host: string
  port: string
  user: string
  socket: string
}

export const DEFAULT_DOCKER_SOCKET = '/var/run/docker.sock'

/** the WSL setup: dockerd in the distro listens on loopback, WSL forwards it to Windows */
export const WSL_DOCKER_PORT = '2375'

/** the parts of a stored endpoint; an empty or unknown one reads as a fresh ssh form.
 * The WSL address reads as WSL only where the router offers it. */
export function parseDockerEndpoint(raw: string, connectionKinds: DockerEndpointKind[]): DockerEndpointParts {
  const empty: DockerEndpointParts = { kind: 'ssh', host: '', port: '22', user: '', socket: DEFAULT_DOCKER_SOCKET }
  const match = /^(ssh|unix|tcp):\/\/(.*)$/.exec(raw.trim())
  if (!match) return empty
  const [, kind, rest] = match
  if (kind === 'unix') return { ...empty, kind: 'unix', socket: rest || DEFAULT_DOCKER_SOCKET }
  const slash = rest.indexOf('/')
  const authority = slash >= 0 ? rest.slice(0, slash) : rest
  const path = slash >= 0 ? rest.slice(slash) : ''
  const at = authority.lastIndexOf('@')
  const user = at >= 0 ? authority.slice(0, at) : ''
  const hostPort = at >= 0 ? authority.slice(at + 1) : authority
  const colon = hostPort.lastIndexOf(':')
  const host = colon >= 0 ? hostPort.slice(0, colon) : hostPort
  const port = colon >= 0 ? hostPort.slice(colon + 1) : kind === 'ssh' ? '22' : '2376'
  if (kind === 'tcp' && host === '127.0.0.1' && port === WSL_DOCKER_PORT && connectionKinds.includes('wsl')) {
    return { ...empty, kind: 'wsl', host, port }
  }
  return { kind: kind as DockerEndpointKind, host, port, user, socket: path || DEFAULT_DOCKER_SOCKET }
}

export function composeDockerEndpoint(parts: DockerEndpointParts): string {
  switch (parts.kind) {
    case 'unix':
      return `unix://${parts.socket.trim() || DEFAULT_DOCKER_SOCKET}`
    case 'wsl':
      return `tcp://127.0.0.1:${WSL_DOCKER_PORT}`
    case 'tcp':
      return `tcp://${parts.host.trim()}:${parts.port.trim()}`
    case 'ssh': {
      const socket = parts.socket.trim() && parts.socket.trim() !== DEFAULT_DOCKER_SOCKET ? parts.socket.trim() : ''
      const port = parts.port.trim() && parts.port.trim() !== '22' ? `:${parts.port.trim()}` : ''
      return `ssh://${parts.user.trim()}@${parts.host.trim()}${port}${socket}`
    }
  }
}
