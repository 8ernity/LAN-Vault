package com.example.lanvault.protocol

import java.io.EOFException
import java.io.InputStream
import java.io.OutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.charset.StandardCharsets
import java.security.MessageDigest

object Protocol {
    val MAGIC = byteArrayOf(0x4c, 0x56, 0x01, 0x00) // "LV\x01\x00"
    const val DEFAULT_PORT = 47800
    const val BEACON_PORT = 47801
    const val BEACON_INTERVAL_MS = 2000
    const val CHUNK_SIZE_DEFAULT = 4194304 // 4 MiB
    const val DEFAULT_CHUNK_SIZE = 524288  // 512 KiB
    const val MAJOR_VERSION = 1
    const val MINOR_VERSION = 1

    const val FRAME_HELLO: Byte = 1
    const val FRAME_HELLO_OK: Byte = 2
    const val FRAME_OFFER: Byte = 16
    const val FRAME_MANIFEST_PART: Byte = 17
    const val FRAME_MANIFEST_END: Byte = 18
    const val FRAME_ACCEPT: Byte = 19
    const val FRAME_REJECT: Byte = 20
    const val FRAME_DATA: Byte = 32
    const val FRAME_CHUNK_ACK: Byte = 33
    const val FRAME_FILE_DONE: Byte = 34
    const val FRAME_DONE: Byte = 64
    const val FRAME_DONE_OK: Byte = 65
    const val FRAME_PING: Byte = 125
    const val FRAME_PONG: Byte = 126
    const val FRAME_ERROR: Byte = 127

    const val MAX_CONTROL_FRAME_SIZE = 1048576 // 1 MiB
    const val MAX_DATA_FRAME_SIZE = 16777257   // 16 MiB + 41
}

data class ManifestEntry(
    val index: Int,
    val type: String, // "file" or "dir"
    val path: String,
    val size: Long,
    val mtimeMs: Long
)

data class DecodedFrame(
    val type: Byte,
    val payload: ByteArray
) {
    fun payloadAsString(): String = String(payload, StandardCharsets.UTF_8)
}

object Framing {
    /**
     * Reads a wire frame:
     * Length (u32, 4 bytes big endian) = 1 (type byte) + payload.size
     * Type (u8, 1 byte)
     * Payload (length - 1 bytes)
     */
    fun readFrame(stream: InputStream): DecodedFrame {
        val lenBuf = ByteArray(4)
        var read = 0
        while (read < 4) {
            val n = stream.read(lenBuf, read, 4 - read)
            if (n == -1) throw EOFException("Premature EOF reading frame length")
            read += n
        }
        val totalLength = ByteBuffer.wrap(lenBuf).order(ByteOrder.BIG_ENDIAN).int
        if (totalLength < 1) throw java.io.IOException("Invalid wire frame length: $totalLength")

        val typeInt = stream.read()
        if (typeInt == -1) throw EOFException("Premature EOF reading frame type")
        val type = typeInt.toByte()

        val payloadLen = totalLength - 1
        val payload = ByteArray(payloadLen)
        var pRead = 0
        while (pRead < payloadLen) {
            val n = stream.read(payload, pRead, payloadLen - pRead)
            if (n == -1) throw EOFException("Premature EOF reading frame payload ($pRead/$payloadLen)")
            pRead += n
        }
        return DecodedFrame(type, payload)
    }

    /**
     * Writes length (u32) + type (u8) + payload bytes
     */
    fun writeRawFrame(stream: OutputStream, frameType: Byte, payload: ByteArray) {
        val totalLength = payload.size + 1
        val header = ByteBuffer.allocate(5).order(ByteOrder.BIG_ENDIAN)
        header.putInt(totalLength)
        header.put(frameType)
        synchronized(stream) {
            stream.write(header.array())
            if (payload.isNotEmpty()) {
                stream.write(payload)
            }
            stream.flush()
        }
    }

    fun writeJSONFrame(stream: OutputStream, frameType: Byte, json: String) {
        writeRawFrame(stream, frameType, json.toByteArray(StandardCharsets.UTF_8))
    }

    /**
     * Encode DATA frame payload:
     * FileIndex (u32, 4 bytes) + ChunkIndex (u32, 4 bytes) + Data (N bytes) + SHA-256 (32 bytes)
     */
    fun encodeDataFramePayload(fileIndex: Int, chunkIndex: Int, data: ByteArray): ByteArray {
        val hash = MessageDigest.getInstance("SHA-256").digest(data)
        val buf = ByteBuffer.allocate(8 + data.size + 32).order(ByteOrder.BIG_ENDIAN)
        buf.putInt(fileIndex)
        buf.putInt(chunkIndex)
        buf.put(data)
        buf.put(hash)
        return buf.array()
    }

    fun toHex(bytes: ByteArray): String {
        val sb = StringBuilder(bytes.size * 2)
        for (b in bytes) {
            sb.append(String.format("%02x", b))
        }
        return sb.toString()
    }

    fun computeManifestHash(entries: List<ManifestEntry>): String {
        val md = MessageDigest.getInstance("SHA-256")
        for (entry in entries) {
            val record = "${entry.type}\t${entry.path}\t${entry.size}\t${entry.mtimeMs}\n"
            md.update(record.toByteArray(StandardCharsets.UTF_8))
        }
        return toHex(md.digest())
    }
}
