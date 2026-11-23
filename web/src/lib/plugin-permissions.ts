// What an update changes in the permissions the operator approved.
import type { PluginPermissions } from './types'

export interface PermissionDiff {
  added: string[]
  removed: string[]
  dockerChanged: boolean
}

export function diffPermissions(current: PluginPermissions, next: PluginPermissions): PermissionDiff {
  const currentHosts = new Set(current.allow_hosts)
  const nextHosts = new Set(next.allow_hosts)
  return {
    added: next.allow_hosts.filter((host) => !currentHosts.has(host)),
    removed: current.allow_hosts.filter((host) => !nextHosts.has(host)),
    dockerChanged: JSON.stringify(current.docker) !== JSON.stringify(next.docker),
  }
}
