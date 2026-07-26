import { describe, expect, it } from 'vitest'
import { isStaleDeploySession, mayApplyDeployResult } from './quickDeploySession'

// Models what useVMQuickDeploy's token effect and deploy() do to the session
// generation, so the logout/login transitions can be exercised without a DOM.
// The hook bumps the generation on every token change; deploy() captures it at
// the start and re-checks it after each await.
function newSession() {
  let generation = 0
  return {
    // The token-keyed effect: fires on logout and again on login.
    changeToken: () => { generation += 1 },
    startDeploy: () => {
      const startedAt = generation
      return {
        isStale: () => isStaleDeploySession(startedAt, generation),
        mayApply: () => mayApplyDeployResult(startedAt, generation)
      }
    }
  }
}

describe('quick deploy session generation', () => {
  it('lets a deploy finish in the session that started it', () => {
    const session = newSession()
    const deploy = session.startDeploy()

    expect(deploy.isStale()).toBe(false)
    expect(deploy.mayApply()).toBe(true)
  })

  // The reported case: log out mid-deploy, log back in before the request
  // settles. The continuation must not upsert the previous session's VM into
  // the new user's list, toast them about a deploy they never started, or
  // clear a busy flag the new session may own.
  it('discards a deploy that spans a logout and a re-login', () => {
    const session = newSession()
    const deploy = session.startDeploy()

    session.changeToken() // logout
    expect(deploy.isStale()).toBe(true)

    session.changeToken() // login again
    expect(deploy.isStale()).toBe(true)
    expect(deploy.mayApply()).toBe(false)
  })

  it('discards a deploy after a logout alone', () => {
    const session = newSession()
    const deploy = session.startDeploy()

    session.changeToken()

    expect(deploy.mayApply()).toBe(false)
  })

  // Logging back into the same account is still a different session, so the
  // generation must not be derived from the token value.
  it('treats a re-login as a new session even for an identical token', () => {
    const session = newSession()
    const deploy = session.startDeploy()

    session.changeToken()
    session.changeToken()

    expect(deploy.mayApply()).toBe(false)
  })

  // A deploy started after the transition owns the busy flag; the stale one
  // finishing later must not clear it out from under this one.
  it('keeps a deploy started in the new session valid', () => {
    const session = newSession()
    const stale = session.startDeploy()

    session.changeToken()
    session.changeToken()
    const fresh = session.startDeploy()

    expect(stale.mayApply()).toBe(false)
    expect(fresh.mayApply()).toBe(true)
  })

  it('keeps concurrent deploys in one session valid', () => {
    const session = newSession()
    const first = session.startDeploy()
    const second = session.startDeploy()

    expect(first.mayApply()).toBe(true)
    expect(second.mayApply()).toBe(true)
  })
})
