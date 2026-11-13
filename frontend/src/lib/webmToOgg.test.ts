import { describe, expect, it } from 'vitest'
import { oggCrc, oggOpusDuration, opusPacketSamples, webmToOgg } from './webmToOgg'

// --- Construcción de un WebM mínimo como el de MediaRecorder -----------------------------

function idBytes(id: number): number[] {
  const out: number[] = []
  let v = id
  while (v > 0) {
    out.unshift(v & 0xff)
    v = Math.floor(v / 256)
  }
  return out
}
function sizeBytes(size: number): number[] {
  // Siempre en 4 bytes (0x1 + 28 bits)
  return [0x10 | ((size >> 24) & 0x0f), (size >> 16) & 0xff, (size >> 8) & 0xff, size & 0xff]
}
const el = (id: number, data: number[]) => [...idBytes(id), ...sizeBytes(data.length), ...data]
const unknownSize = (id: number) => [...idBytes(id), 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff]
const ascii = (s: string) => [...new TextEncoder().encode(s)]

function opusHead(preSkip: number, channels = 1): number[] {
  return [
    ...ascii('OpusHead'),
    1,
    channels,
    preSkip & 0xff,
    preSkip >> 8,
    0x80,
    0xbb,
    0,
    0,
    0,
    0,
    0
  ]
}

function fakeWebm(packets: number[][]): Uint8Array {
  const simpleBlocks = packets.flatMap((p, i) => el(0xa3, [0x81, 0, i, 0x80, ...p]))
  return new Uint8Array([
    ...el(0x1a45dfa3, [0x42, 0x82, 0x84, ...ascii('webm')]),
    ...unknownSize(0x18538067), // Segment de tamaño desconocido (grabación en vivo)
    ...el(0x1549a966, [0x2a, 0xd7, 0xb1, 0x83, 0x0f, 0x42, 0x40]), // Info (se salta)
    ...el(0x1654ae6b, [
      ...el(0xae, [
        ...el(0xd7, [1]),
        ...el(0x86, ascii('A_OPUS')),
        ...el(0x63a2, opusHead(312)),
        ...el(0xe1, [...el(0x9f, [1])])
      ])
    ]),
    ...unknownSize(0x1f43b675), // Cluster de tamaño desconocido
    ...el(0xe7, [0]),
    ...simpleBlocks
  ])
}

function readPages(ogg: Uint8Array) {
  const view = new DataView(ogg.buffer, ogg.byteOffset, ogg.byteLength)
  const pages: {
    flags: number
    granule: number
    seq: number
    packets: Uint8Array[]
    crcOk: boolean
  }[] = []
  let pos = 0
  while (pos < ogg.length) {
    expect(new TextDecoder().decode(ogg.subarray(pos, pos + 4))).toBe('OggS')
    const segs = ogg[pos + 26]
    const lacing = [...ogg.subarray(pos + 27, pos + 27 + segs)]
    const bodyLen = lacing.reduce((a, b) => a + b, 0)
    const page = ogg.slice(pos, pos + 27 + segs + bodyLen)
    const storedCrc = view.getUint32(pos + 22, true)
    page.set([0, 0, 0, 0], 22)
    const packets: Uint8Array[] = []
    let off = pos + 27 + segs
    let current: number[] = []
    for (const l of lacing) {
      current.push(...ogg.subarray(off, off + l))
      off += l
      if (l < 255) {
        packets.push(new Uint8Array(current))
        current = []
      }
    }
    pages.push({
      flags: ogg[pos + 5],
      granule: view.getUint32(pos + 6, true),
      seq: view.getUint32(pos + 18, true),
      packets,
      crcOk: oggCrc(page) === storedCrc
    })
    pos += 27 + segs + bodyLen
  }
  return pages
}

describe('notas de voz: WebM/Opus a OGG/Opus', () => {
  it('calcula la duración de cada paquete Opus por su TOC', () => {
    expect(opusPacketSamples(new Uint8Array([0xf8]))).toBe(960) // CELT 20 ms, 1 trama
    expect(opusPacketSamples(new Uint8Array([0xf9]))).toBe(1920) // 2 tramas
    expect(opusPacketSamples(new Uint8Array([0x1b, 0x03]))).toBe(3 * 2880) // SILK 60 ms ×3
  })

  it('copia los paquetes a páginas Ogg válidas con cabeceras, CRC y granule', () => {
    const packets = Array.from({ length: 120 }, (_, i) => [0xf8, i, (i * 7) & 0xff, 1, 2, 3])
    const pages = readPages(webmToOgg(fakeWebm(packets)))

    expect(pages.every((p) => p.crcOk)).toBe(true)
    expect(pages.map((p) => p.seq)).toEqual(pages.map((_, i) => i))
    expect(pages[0].flags).toBe(0x02) // inicio de flujo
    expect(new TextDecoder().decode(pages[0].packets[0].subarray(0, 8))).toBe('OpusHead')
    expect(new TextDecoder().decode(pages[1].packets[0].subarray(0, 8))).toBe('OpusTags')
    expect(pages[pages.length - 1].flags).toBe(0x04) // fin de flujo
    expect(pages[pages.length - 1].granule).toBe(312 + 120 * 960)

    const audio = pages.slice(2).flatMap((p) => p.packets.map((x) => [...x]))
    expect(audio).toEqual(packets)

    expect(oggOpusDuration(webmToOgg(fakeWebm(packets)))).toBeCloseTo(2.4, 5)
  })

  it('separa en varios segmentos los paquetes de más de 255 bytes', () => {
    const big = [0xf8, ...Array.from({ length: 600 }, (_, i) => i & 0xff)]
    const pages = readPages(webmToOgg(fakeWebm([big, [0xf8, 1]])))
    expect(pages.slice(2).flatMap((p) => p.packets.map((x) => x.length))).toEqual([601, 2])
  })

  it('rechaza grabaciones vacías', () => {
    expect(() => webmToOgg(fakeWebm([]))).toThrow(/vacía/)
  })
})
