import type { OSImage } from '../types'

export type DeploymentTarget = 'vm' | 'baremetal'

export function effectiveImageFormat(image: OSImage): OSImage['format'] {
  return image.manifest?.root?.format ?? image.format
}

export function supportsDeploymentTarget(image: OSImage, target: DeploymentTarget): boolean {
  const targets = image.manifest?.capabilities?.deployTargets
  if (targets && targets.length > 0) {
    return targets.includes(target)
  }
  const format = effectiveImageFormat(image)
  if (target === 'vm') {
    return format === 'qcow2' && (!image.variant || image.variant === 'cloud')
  }
  return format === 'qcow2' && (
    image.variant === 'baremetal' ||
    Boolean(image.manifest?.root?.path && image.manifest?.root?.rootPartition?.number)
  )
}
