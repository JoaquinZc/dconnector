/**
 * Convierte una grabación WebM/Opus (lo que graba Chromium con MediaRecorder) en OGG/Opus,
 * el formato de las notas de voz de WhatsApp. No recodifica: copia los paquetes Opus a
 * páginas Ogg (RFC 7845), así que es instantáneo y sin pérdida.
 */

const ID = {
  EBML: 0x1a45dfa3,
  Segment: 0x18538067,
  Tracks: 0x1654ae6b,
  TrackEntry: 0xae,
  TrackNumber: 0xd7,
  CodecID: 0x86,
  CodecPrivate: 0x63a2,
  Audio: 0xe1,
  Channels: 0x9f,
  Cluster: 0x1f43b675,
  BlockGroup: 0xa0,
  Block: 0xa1,
  SimpleBlock: 0xa3
}

/** Elementos contenedores en los que se entra (el resto se salta). */
const ENTER = new Set([ID.Segment, ID.Tracks, ID.TrackEntry, ID.Audio, ID.Cluster, ID.BlockGroup])

function readVint(buf: Uint8Array, pos: number, keepMarker: boolean) {
  const first = buf[pos]
  if (first === undefined) return null
  let length = 1
  let mask = 0x80
  while (length <= 8 && !(first & mask)) {
    length++
    mask >>= 1
  }
  if (length > 8 || pos + length > buf.length) return null
  let value = keepMarker ? first : first & (mask - 1)
  let allOnes = (first & (mask - 1)) === mask - 1
  for (let i = 1; i < length; i++) {
    value = value * 256 + buf[pos + i]
    if (buf[pos + i] !== 0xff) allOnes = false
  }
  return { value, length, unknown: !keepMarker && allOnes }
}

function readUint(buf: Uint8Array, start: number, size: number): number {
  let v = 0
  for (let i = 0; i < size; i++) v = v * 256 + buf[start + i]
  return v
}

/** Muestras (a 48 kHz) que contiene un paquete Opus, según su byte TOC. */
export function opusPacketSamples(packet: Uint8Array): number {
  if (packet.length === 0) return 0
  const toc = packet[0]
  const config = toc >> 3
  const code = toc & 3
  const frameSize =
    config < 12
      ? [480, 960, 1920, 2880][config % 4]
      : config < 16
        ? [480, 960][config % 2]
        : [120, 240, 480, 960][config % 4]
  const frames = code === 0 ? 1 : code === 3 ? (packet[1] ?? 0) & 0x3f : 2
  return frames * frameSize
}

interface ParsedWebm {
  packets: Uint8Array[]
  opusHead: Uint8Array | null
  channels: number
}

function parseWebm(buf: Uint8Array): ParsedWebm {
  const packets: Uint8Array[] = []
  let opusHead: Uint8Array | null = null
  let channels = 1
  let codecOk = true
  let pos = 0
  while (pos < buf.length) {
    const id = readVint(buf, pos, true)
    if (!id) break
    const size = readVint(buf, pos + id.length, false)
    if (!size) break
    const start = pos + id.length + size.length
    if (ENTER.has(id.value)) {
      pos = start
      continue
    }
    if (size.unknown) break
    const end = Math.min(buf.length, start + size.value)
    switch (id.value) {
      case ID.CodecID:
        codecOk = new TextDecoder().decode(buf.subarray(start, end)) === 'A_OPUS'
        break
      case ID.CodecPrivate:
        opusHead = buf.slice(start, end)
        break
      case ID.Channels:
        channels = readUint(buf, start, end - start) || 1
        break
      case ID.SimpleBlock:
      case ID.Block: {
        const track = readVint(buf, start, false)
        if (!track) break
        const flags = buf[start + track.length + 2]
        if ((flags >> 1) & 3) throw new Error('Grabación no compatible (bloques agrupados).')
        const data = buf.slice(start + track.length + 3, end)
        if (data.length) packets.push(data)
        break
      }
    }
    pos = start + size.value
  }
  if (!codecOk) throw new Error('La grabación no es Opus.')
  if (packets.length === 0) throw new Error('La grabación está vacía.')
  return { packets, opusHead, channels }
}

// ------------------------------------------------------------------ Ogg

const CRC_TABLE = (() => {
  const table = new Uint32Array(256)
  for (let i = 0; i < 256; i++) {
    let r = i << 24
    for (let j = 0; j < 8; j++) r = r & 0x80000000 ? (r << 1) ^ 0x04c11db7 : r << 1
    table[i] = r >>> 0
  }
  return table
})()

export function oggCrc(data: Uint8Array): number {
  let crc = 0
  for (const byte of data) crc = ((crc << 8) ^ CRC_TABLE[((crc >>> 24) ^ byte) & 0xff]) >>> 0
  return crc
}

function oggPage(
  packets: Uint8Array[],
  granule: number,
  serial: number,
  sequence: number,
  flags: number
): Uint8Array {
  const lacing: number[] = []
  for (const p of packets) {
    let n = p.length
    while (n >= 255) {
      lacing.push(255)
      n -= 255
    }
    lacing.push(n)
  }
  const bodyLength = packets.reduce((n, p) => n + p.length, 0)
  const page = new Uint8Array(27 + lacing.length + bodyLength)
  const view = new DataView(page.buffer)
  page.set([0x4f, 0x67, 0x67, 0x53], 0) // "OggS"
  page[4] = 0
  page[5] = flags
  view.setUint32(6, granule % 0x100000000, true)
  view.setUint32(10, Math.floor(granule / 0x100000000), true)
  view.setUint32(14, serial, true)
  view.setUint32(18, sequence, true)
  page[26] = lacing.length
  page.set(lacing, 27)
  let offset = 27 + lacing.length
  for (const p of packets) {
    page.set(p, offset)
    offset += p.length
  }
  view.setUint32(22, oggCrc(page), true)
  return page
}

function buildOpusHead(channels: number): Uint8Array {
  const head = new Uint8Array(19)
  const view = new DataView(head.buffer)
  head.set(new TextEncoder().encode('OpusHead'), 0)
  head[8] = 1
  head[9] = channels
  view.setUint16(10, 312, true) // pre-skip típico del codificador de Chromium
  view.setUint32(12, 48000, true)
  return head
}

function buildOpusTags(): Uint8Array {
  const vendor = new TextEncoder().encode("D'Ventas")
  const tags = new Uint8Array(8 + 4 + vendor.length + 4)
  const view = new DataView(tags.buffer)
  tags.set(new TextEncoder().encode('OpusTags'), 0)
  view.setUint32(8, vendor.length, true)
  tags.set(vendor, 12)
  view.setUint32(12 + vendor.length, 0, true)
  return tags
}

export function webmToOgg(webm: Uint8Array, serial = 0x5741434d): Uint8Array {
  const { packets, opusHead, channels } = parseWebm(webm)
  const isHead = opusHead && new TextDecoder().decode(opusHead.subarray(0, 8)) === 'OpusHead'
  const head = isHead ? opusHead! : buildOpusHead(channels)
  const preSkip = new DataView(head.buffer, head.byteOffset).getUint16(10, true)

  const pages: Uint8Array[] = [
    oggPage([head], 0, serial, 0, 0x02),
    oggPage([buildOpusTags()], 0, serial, 1, 0)
  ]
  let granule = preSkip
  let sequence = 2
  let batch: Uint8Array[] = []
  let segments = 0
  packets.forEach((p, i) => {
    const needed = Math.floor(p.length / 255) + 1
    if (batch.length && (segments + needed > 255 || batch.length >= 50)) {
      pages.push(oggPage(batch, granule, serial, sequence++, 0))
      batch = []
      segments = 0
    }
    batch.push(p)
    segments += needed
    granule += opusPacketSamples(p)
    if (i === packets.length - 1) pages.push(oggPage(batch, granule, serial, sequence++, 0x04))
  })

  const total = pages.reduce((n, p) => n + p.length, 0)
  const out = new Uint8Array(total)
  let offset = 0
  for (const p of pages) {
    out.set(p, offset)
    offset += p.length
  }
  return out
}

/** Duración en segundos de un OGG/Opus (granule de la última página menos el pre-skip). */
export function oggOpusDuration(ogg: Uint8Array): number {
  const view = new DataView(ogg.buffer, ogg.byteOffset, ogg.byteLength)
  let pos = 0
  let lastGranule = 0
  let preSkip = 0
  while (pos + 27 <= ogg.length) {
    const segments = ogg[pos + 26]
    const granule = view.getUint32(pos + 6, true) + view.getUint32(pos + 10, true) * 0x100000000
    let body = 0
    for (let i = 0; i < segments; i++) body += ogg[pos + 27 + i]
    if (pos === 0) preSkip = view.getUint16(pos + 27 + segments + 10, true)
    lastGranule = granule
    pos += 27 + segments + body
  }
  return Math.max(0, (lastGranule - preSkip) / 48000)
}
