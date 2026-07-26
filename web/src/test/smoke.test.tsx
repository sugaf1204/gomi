import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

describe('test infrastructure', () => {
  it('renders React components into jsdom', () => {
    render(<p>gomi</p>)
    expect(screen.getByText('gomi')).toBeInTheDocument()
  })
})
