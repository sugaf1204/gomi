// useVMQuickDeploy is mounted above App's unauthenticated early return, so it
// survives a logout and its in-flight deploy outlives the session that started
// it. The deploy captures the generation it began in and re-checks it after
// every await; a mismatch means the continuation belongs to a session that is
// over and must not touch the current one.
//
// The generation is a plain counter rather than the token itself: logging back
// into the same account issues a new session that is still not the one the
// deploy was started in, and an identical token would make that transition
// invisible.
export function isStaleDeploySession(startedGeneration: number, currentGeneration: number): boolean {
  return startedGeneration !== currentGeneration
}

// Whether a deploy continuation may still write to shared state — upserting its
// VM, refreshing, toasting, or clearing the busy flag.
export function mayApplyDeployResult(startedGeneration: number, currentGeneration: number): boolean {
  return !isStaleDeploySession(startedGeneration, currentGeneration)
}
