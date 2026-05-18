import { describe, expect, it } from 'vitest'

import { scanMediaRefs, scenariosReferencingMedia, validateMediaRefs } from '@/lib/mediaRefs'

describe('mediaRefs', () => {
  it('finds wav aliases', () => {
    const refs = scanMediaRefs('play [[media:wav/hello.wav]] now')
    expect(refs).toEqual([{ kind: 'wav', name: 'hello.wav', raw: '[[media:wav/hello.wav]]' }])
  })

  it('flags missing media', () => {
    const r = validateMediaRefs('[[media:wav/missing]]', { wav: new Set(['ok.wav']), pcap: new Set() })
    expect(r.missing).toHaveLength(1)
    expect(r.valid).toHaveLength(0)
  })

  it('finds scenario references', () => {
    const ids = scenariosReferencingMedia(
      [{ id: 'a', xml: 'x [[media:wav/tone]] y' }, { id: 'b', xml: 'none' }],
      'wav',
      'tone',
    )
    expect(ids).toEqual(['a'])
  })
})
