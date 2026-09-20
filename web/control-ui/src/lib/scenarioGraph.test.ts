import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import {
  addBlock,
  blocksToXML,
  canAddBlock,
  commandTypes,
  edgeLabel,
  emptyBlocks,
  snapToGrid,
  xmlToBlocks,
} from './scenarioGraph'

const STARTER = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="my_scenario">
  <recv request="INVITE" />
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[call_number]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0

    ]]>
  </send>
</scenario>
`

describe('scenarioGraph', () => {
  it('snaps to the 16px grid', () => {
    expect(snapToGrid(7)).toBe(0)
    expect(snapToGrid(8)).toBe(16)
    expect(snapToGrid(24)).toBe(32)
  })

  it('round-trips the starter UAS XML', () => {
    const blocks = xmlToBlocks(STARTER)
    expect(blocks[0]?.type).toBe('start')
    expect(blocks[0]?.name).toBe('my_scenario')
    expect(commandTypes(blocks)).toEqual(['recv', 'send'])
    expect(blocks[1]?.request).toBe('INVITE')
    expect(blocks[2]?.text).toMatch(/SIP\/2\.0 200 OK/)
    const xml = blocksToXML(blocks)
    const again = xmlToBlocks(xml)
    expect(commandTypes(again)).toEqual(['recv', 'send'])
    expect(again[2]?.text).toMatch(/SIP\/2\.0 200 OK/)
  })

  it('round-trips basic_uac send/recv/pause chain', () => {
    const here = dirname(fileURLToPath(import.meta.url))
    const xml = readFileSync(join(here, '../../../../testdata/scenarios/basic_uac.xml'), 'utf8')
    const blocks = xmlToBlocks(xml)
    expect(blocks[0]?.name).toBe('Test UAC')
    expect(commandTypes(blocks)).toEqual(['send', 'recv', 'recv', 'send', 'pause', 'send', 'recv'])
    expect(blocks[1]?.retrans).toBe('200')
    expect(blocks[1]?.text).toMatch(/^INVITE /)
    expect(blocks[2]?.response).toBe('100')
    expect(blocks[2]?.optional).toBe(true)
    expect(blocks[5]?.type).toBe('pause')
    expect(blocks[5]?.milliseconds).toBe(50)
    const out = blocksToXML(blocks)
    const again = xmlToBlocks(out)
    expect(commandTypes(again)).toEqual(commandTypes(blocks))
    expect(again[1]?.text).toMatch(/^INVITE /)
    expect(again[6]?.text).toMatch(/^BYE /)
  })

  it('keeps nested action XML as raw so it is not dropped', () => {
    const xml = `<scenario name="x"><send><![CDATA[INVITE]]><action><exec rtp_stream="echo"/></action></send></scenario>`
    const blocks = xmlToBlocks(xml)
    expect(commandTypes(blocks)).toEqual(['raw'])
    expect(blocks[1]?.xml).toMatch(/<action>/)
    expect(blocksToXML(blocks)).toMatch(/<action>/)
  })

  it('labels edges with recv codes and pause times', () => {
    const blocks = xmlToBlocks(STARTER)
    const pause = { key: 'p', type: 'pause' as const, milliseconds: 1500 }
    expect(edgeLabel(blocks[0]!, blocks[1]!)).toBe('INVITE')
    expect(edgeLabel(blocks[1]!, blocks[2]!)).toMatch(/200/)
    expect(edgeLabel(blocks[2]!, pause)).toBe('1500 ms')
  })

  it('allows many send/recv blocks but only one start', () => {
    let blocks = emptyBlocks()
    expect(canAddBlock(blocks, 'start')).toBe(false)
    expect(canAddBlock(blocks, 'send')).toBe(true)
    blocks = addBlock(blocks, 'pause')
    expect(commandTypes(blocks)).toEqual(['recv', 'send', 'pause'])
  })

  it('parses kefir lab one_way.xml and keeps rtp_stream as raw', () => {
    const here = dirname(fileURLToPath(import.meta.url))
    const xml = readFileSync(join(here, '../../../../internal/scenario/lab/one_way.xml'), 'utf8')
    const blocks = xmlToBlocks(xml)
    expect(blocks[0]?.name).toBe('one_way')
    expect(commandTypes(blocks)).toEqual([
      'send',
      'recv',
      'recv',
      'recv',
      'recv',
      'raw',
      'send',
      'pause',
      'raw',
      'send',
      'recv',
    ])
    expect(blocks[1]?.text).toMatch(/a=sendonly/)
    expect(blocks[6]?.xml).toMatch(/rtp_stream="synthetic/)
    const out = blocksToXML(blocks)
    expect(out).toMatch(/rtp_stream="synthetic/)
    expect(out).toMatch(/rtp_stream="stop"/)
    expect(commandTypes(xmlToBlocks(out))).toEqual(commandTypes(blocks))
  })
})
